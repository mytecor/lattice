package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

var version = "dev"

type metadata struct {
	Node         string  `json:"node"`
	Service      string  `json:"service"`
	Generation   *int64  `json:"generation"`
	Commit       *string `json:"commit"`
	Kernel       string  `json:"kernel"`
	StateVersion string  `json:"stateVersion"`
	ActivatedAt  int64   `json:"activatedAt"`
}

type systemSnapshot struct {
	UptimeSeconds        float64           `json:"uptimeSeconds"`
	Load1                float64           `json:"load1"`
	Load5                float64           `json:"load5"`
	Load15               float64           `json:"load15"`
	CPUUtilization       float64           `json:"cpuUtilization"`
	MemoryTotalBytes     uint64            `json:"memoryTotalBytes"`
	MemoryAvailableBytes uint64            `json:"memoryAvailableBytes"`
	MemoryUsedBytes      uint64            `json:"memoryUsedBytes"`
	RootTotalBytes       uint64            `json:"rootTotalBytes"`
	RootAvailableBytes   uint64            `json:"rootAvailableBytes"`
	Services             map[string]string `json:"services"`
}

type statusResponse struct {
	metadata
	System systemSnapshot `json:"system"`
}

type cpuTimes struct {
	User, Nice, System, Idle, IOWait, IRQ, SoftIRQ, Steal float64
}

func (c cpuTimes) total() float64 {
	return c.User + c.Nice + c.System + c.Idle + c.IOWait + c.IRQ + c.SoftIRQ + c.Steal
}
func (c cpuTimes) busy() float64 { return c.total() - c.Idle - c.IOWait }

type memoryStats struct {
	Total, Available, SwapTotal, SwapFree uint64
}

type netStats struct {
	ReceiveBytes, ReceivePackets, ReceiveErrors, ReceiveDrops     uint64
	TransmitBytes, TransmitPackets, TransmitErrors, TransmitDrops uint64
}

type diskStats struct {
	Reads, ReadSectors, Writes, WriteSectors, IOTimeMS uint64
}

type unitStats struct {
	State    string
	Restarts uint64
}

type collector struct {
	procRoot, sysRoot, rootMount, systemctl string
	units                                   []string
	mu                                      sync.Mutex
	lastCPU                                 cpuTimes
	haveCPU                                 bool
	cpuUtil                                 float64
	errors                                  atomic.Uint64
}

func (c *collector) read(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		c.errors.Add(1)
	}
	return b, err
}

func parseCPU(r io.Reader) (cpuTimes, error) {
	s := bufio.NewScanner(r)
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) < 9 || f[0] != "cpu" {
			continue
		}
		v := make([]float64, 8)
		for i := range v {
			n, err := strconv.ParseFloat(f[i+1], 64)
			if err != nil {
				return cpuTimes{}, err
			}
			// Linux exports /proc/stat in USER_HZ; all supported NixOS targets use
			// the kernel ABI value 100 (sysconf(_SC_CLK_TCK)).
			v[i] = n / 100
		}
		return cpuTimes{v[0], v[1], v[2], v[3], v[4], v[5], v[6], v[7]}, nil
	}
	return cpuTimes{}, errors.New("aggregate cpu line not found")
}

func parseMeminfo(r io.Reader) (memoryStats, error) {
	var m memoryStats
	s := bufio.NewScanner(r)
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		n *= 1024
		switch strings.TrimSuffix(f[0], ":") {
		case "MemTotal":
			m.Total = n
		case "MemAvailable":
			m.Available = n
		case "SwapTotal":
			m.SwapTotal = n
		case "SwapFree":
			m.SwapFree = n
		}
	}
	if m.Total == 0 {
		return m, errors.New("MemTotal not found")
	}
	return m, s.Err()
}

func parseNetdev(r io.Reader) map[string]netStats {
	out := map[string]netStats{}
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		name := strings.TrimSpace(parts[0])
		if name == "lo" {
			continue
		}
		f := strings.Fields(parts[1])
		if len(f) < 16 {
			continue
		}
		vals := make([]uint64, 16)
		ok := true
		for i := range vals {
			var err error
			vals[i], err = strconv.ParseUint(f[i], 10, 64)
			if err != nil {
				ok = false
			}
		}
		if ok {
			out[name] = netStats{vals[0], vals[1], vals[2], vals[3], vals[8], vals[9], vals[10], vals[11]}
		}
	}
	return out
}

func parseDiskStat(s string) (diskStats, error) {
	f := strings.Fields(s)
	if len(f) < 11 {
		return diskStats{}, errors.New("short disk stat")
	}
	v := make([]uint64, len(f))
	for i := range f {
		n, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			return diskStats{}, err
		}
		v[i] = n
	}
	return diskStats{Reads: v[0], ReadSectors: v[2], Writes: v[4], WriteSectors: v[6], IOTimeMS: v[9]}, nil
}

func (c *collector) cpu() (cpuTimes, float64, error) {
	b, err := c.read(filepath.Join(c.procRoot, "stat"))
	if err != nil {
		return cpuTimes{}, 0, err
	}
	now, err := parseCPU(strings.NewReader(string(b)))
	if err != nil {
		c.errors.Add(1)
		return cpuTimes{}, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.haveCPU {
		dt, db := now.total()-c.lastCPU.total(), now.busy()-c.lastCPU.busy()
		if dt > 0 {
			c.cpuUtil = db / dt
		}
	}
	c.lastCPU, c.haveCPU = now, true
	return now, c.cpuUtil, nil
}

func (c *collector) memory() (memoryStats, error) {
	b, err := c.read(filepath.Join(c.procRoot, "meminfo"))
	if err != nil {
		return memoryStats{}, err
	}
	m, err := parseMeminfo(strings.NewReader(string(b)))
	if err != nil {
		c.errors.Add(1)
	}
	return m, err
}

func (c *collector) uptimeLoad() (float64, [3]float64) {
	var up float64
	var load [3]float64
	if b, err := c.read(filepath.Join(c.procRoot, "uptime")); err == nil {
		fmt.Sscan(string(b), &up)
	}
	if b, err := c.read(filepath.Join(c.procRoot, "loadavg")); err == nil {
		fmt.Sscan(string(b), &load[0], &load[1], &load[2])
	}
	return up, load
}

func (c *collector) filesystem() (uint64, uint64, error) {
	var s syscall.Statfs_t
	if err := syscall.Statfs(c.rootMount, &s); err != nil {
		c.errors.Add(1)
		return 0, 0, err
	}
	return s.Blocks * uint64(s.Bsize), s.Bavail * uint64(s.Bsize), nil
}

func (c *collector) networks() map[string]netStats {
	b, err := c.read(filepath.Join(c.procRoot, "net/dev"))
	if err != nil {
		return nil
	}
	return parseNetdev(strings.NewReader(string(b)))
}

func (c *collector) disks() map[string]diskStats {
	out := map[string]diskStats{}
	entries, err := os.ReadDir(filepath.Join(c.sysRoot, "block"))
	if err != nil {
		c.errors.Add(1)
		return out
	}
	for _, e := range entries {
		b, err := c.read(filepath.Join(c.sysRoot, "block", e.Name(), "stat"))
		if err != nil {
			continue
		}
		if d, err := parseDiskStat(string(b)); err == nil {
			out[e.Name()] = d
		} else {
			c.errors.Add(1)
		}
	}
	return out
}

func (c *collector) temperatures() map[string]float64 {
	out := map[string]float64{}
	zones, _ := filepath.Glob(filepath.Join(c.sysRoot, "class/thermal/thermal_zone*"))
	for _, zone := range zones {
		typ, err1 := c.read(filepath.Join(zone, "type"))
		raw, err2 := c.read(filepath.Join(zone, "temp"))
		if err1 != nil || err2 != nil {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(string(raw)), 64)
		if err != nil {
			c.errors.Add(1)
			continue
		}
		out[strings.TrimSpace(string(typ))] = v / 1000
	}
	return out
}

func (c *collector) services() map[string]unitStats {
	out := map[string]unitStats{}
	for _, unit := range c.units {
		cmd := exec.Command(c.systemctl, "show", unit, "--property=ActiveState", "--property=NRestarts", "--value")
		b, err := cmd.Output()
		if err != nil {
			c.errors.Add(1)
			out[unit] = unitStats{State: "unknown"}
			continue
		}
		lines := strings.Split(strings.TrimSpace(string(b)), "\n")
		state := "unknown"
		var restarts uint64
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "active" || line == "inactive" || line == "failed" || line == "activating" || line == "deactivating" || line == "reloading" {
				state = line
			} else if n, err := strconv.ParseUint(line, 10, 64); err == nil {
				restarts = n
			}
		}
		out[unit] = unitStats{state, restarts}
	}
	return out
}

func (c *collector) snapshot() systemSnapshot {
	_, util, _ := c.cpu()
	mem, _ := c.memory()
	up, load := c.uptimeLoad()
	total, avail, _ := c.filesystem()
	units := c.services()
	states := make(map[string]string, len(units))
	for k, v := range units {
		states[k] = v.State
	}
	used := uint64(0)
	if mem.Total >= mem.Available {
		used = mem.Total - mem.Available
	}
	return systemSnapshot{up, load[0], load[1], load[2], util, mem.Total, mem.Available, used, total, avail, states}
}

func label(s string) string { return strconv.Quote(s) }

func (c *collector) metrics(w io.Writer, meta metadata, started time.Time) {
	begin := time.Now()
	cpu, util, _ := c.cpu()
	mem, _ := c.memory()
	up, load := c.uptimeLoad()
	total, avail, _ := c.filesystem()
	write := func(name, help, typ string, value float64, labels string) {
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s %s\n%s%s %g\n", name, help, name, typ, name, labels, value)
	}
	write("node_status_up", "Whether the node-status collector is running.", "gauge", 1, "")
	fmt.Fprintf(w, "# HELP node_status_build_info Build and node metadata.\n# TYPE node_status_build_info gauge\nnode_status_build_info{node=%s,version=%s,kernel=%s,state_version=%s} 1\n", label(meta.Node), label(version), label(meta.Kernel), label(meta.StateVersion))
	write("node_status_process_start_time_seconds", "Unix timestamp when node-status started.", "gauge", float64(started.Unix()), "")
	write("node_status_uptime_seconds", "Seconds since system boot.", "gauge", up, "")
	write("node_status_cpu_utilization_ratio", "CPU busy ratio observed between collections.", "gauge", util, "")
	fmt.Fprintln(w, "# HELP node_status_cpu_seconds_total CPU time by mode.\n# TYPE node_status_cpu_seconds_total counter")
	for _, p := range []struct {
		name  string
		value float64
	}{{"user", cpu.User}, {"nice", cpu.Nice}, {"system", cpu.System}, {"idle", cpu.Idle}, {"iowait", cpu.IOWait}, {"irq", cpu.IRQ}, {"softirq", cpu.SoftIRQ}, {"steal", cpu.Steal}} {
		fmt.Fprintf(w, "node_status_cpu_seconds_total{mode=%s} %g\n", label(p.name), p.value)
	}
	for i, n := range []string{"node_status_load1", "node_status_load5", "node_status_load15"} {
		write(n, "System load average.", "gauge", load[i], "")
	}
	write("node_status_memory_total_bytes", "Total system memory.", "gauge", float64(mem.Total), "")
	write("node_status_memory_available_bytes", "Available system memory.", "gauge", float64(mem.Available), "")
	write("node_status_memory_used_bytes", "Used system memory (total minus available).", "gauge", float64(mem.Total-mem.Available), "")
	write("node_status_swap_total_bytes", "Total swap space.", "gauge", float64(mem.SwapTotal), "")
	write("node_status_swap_free_bytes", "Free swap space.", "gauge", float64(mem.SwapFree), "")
	write("node_status_filesystem_size_bytes", "Root filesystem size.", "gauge", float64(total), `{mountpoint="/"}`)
	write("node_status_filesystem_available_bytes", "Root filesystem bytes available to unprivileged users.", "gauge", float64(avail), `{mountpoint="/"}`)
	fmt.Fprintln(w, "# HELP node_status_network_receive_bytes_total Network bytes received.\n# TYPE node_status_network_receive_bytes_total counter")
	nets := c.networks()
	netNames := make([]string, 0, len(nets))
	for name := range nets {
		netNames = append(netNames, name)
	}
	sort.Strings(netNames)
	for _, name := range netNames {
		n := nets[name]
		ls := "{device=" + label(name) + "}"
		fmt.Fprintf(w, "node_status_network_receive_bytes_total%s %d\nnode_status_network_transmit_bytes_total%s %d\nnode_status_network_receive_errors_total%s %d\nnode_status_network_transmit_errors_total%s %d\nnode_status_network_receive_drops_total%s %d\nnode_status_network_transmit_drops_total%s %d\n", ls, n.ReceiveBytes, ls, n.TransmitBytes, ls, n.ReceiveErrors, ls, n.TransmitErrors, ls, n.ReceiveDrops, ls, n.TransmitDrops)
	}
	disks := c.disks()
	diskNames := make([]string, 0, len(disks))
	for name := range disks {
		diskNames = append(diskNames, name)
	}
	sort.Strings(diskNames)
	for _, name := range diskNames {
		d := disks[name]
		ls := "{device=" + label(name) + "}"
		fmt.Fprintf(w, "node_status_disk_reads_completed_total%s %d\nnode_status_disk_writes_completed_total%s %d\nnode_status_disk_read_bytes_total%s %d\nnode_status_disk_written_bytes_total%s %d\nnode_status_disk_io_time_seconds_total%s %g\n", ls, d.Reads, ls, d.Writes, ls, d.ReadSectors*512, ls, d.WriteSectors*512, ls, float64(d.IOTimeMS)/1000)
	}
	for typ, temp := range c.temperatures() {
		fmt.Fprintf(w, "node_status_thermal_zone_celsius{type=%s} %g\n", label(typ), temp)
	}
	units := c.services()
	unitNames := make([]string, 0, len(units))
	for name := range units {
		unitNames = append(unitNames, name)
	}
	sort.Strings(unitNames)
	fmt.Fprintln(w, "# HELP node_status_systemd_unit_state Current state of a monitored systemd unit.\n# TYPE node_status_systemd_unit_state gauge")
	for _, name := range unitNames {
		u := units[name]
		states := []string{"active", "inactive", "failed", "activating", "deactivating", "reloading", "unknown"}
		for _, state := range states {
			value := 0
			if state == u.State {
				value = 1
			}
			fmt.Fprintf(w, "node_status_systemd_unit_state{unit=%s,state=%s} %d\n", label(name), label(state), value)
		}
		fmt.Fprintf(w, "node_status_systemd_unit_restarts_total{unit=%s} %d\n", label(name), u.Restarts)
	}
	write("node_status_collection_errors_total", "Collector read or command errors.", "counter", float64(c.errors.Load()), "")
	write("node_status_scrape_duration_seconds", "Time spent collecting this metrics response.", "gauge", time.Since(begin).Seconds(), "")
}

func readMetadata(stateVersion, repo, currentSystem, profilesSystem string) metadata {
	host, _ := os.Hostname()
	kernel := runtime.GOOS
	activatedAt := time.Now().Unix()
	if info, err := os.Lstat(currentSystem); err == nil {
		activatedAt = info.ModTime().Unix()
	}
	if b, err := os.ReadFile(filepath.Join("/proc", "sys/kernel/osrelease")); err == nil {
		kernel = strings.TrimSpace(string(b))
	}
	var generation *int64
	for _, path := range []string{profilesSystem, currentSystem} {
		if target, err := os.Readlink(path); err == nil {
			base := filepath.Base(target)
			base = strings.TrimPrefix(base, "system-")
			base = strings.TrimSuffix(base, "-link")
			if n, err := strconv.ParseInt(base, 10, 64); err == nil {
				generation = &n
				break
			}
		}
	}
	var commit *string
	refPath := filepath.Join(repo, "refs/lattice/source")
	if b, err := os.ReadFile(refPath); err == nil {
		s := strings.TrimSpace(string(b))
		if len(s) == 40 {
			commit = &s
		}
	} else if b, err := os.ReadFile(filepath.Join(repo, "packed-refs")); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			f := strings.Fields(line)
			if len(f) == 2 && f[1] == "refs/lattice/source" && len(f[0]) == 40 {
				s := f[0]
				commit = &s
			}
		}
	}
	return metadata{host, "lattice-node-status", generation, commit, kernel, stateVersion, activatedAt}
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func main() {
	listen := flag.String("listen", env("NODE_STATUS_LISTEN_ADDRESS", "127.0.0.1:9217"), "HTTP listen address")
	flag.Parse()
	started := time.Now()
	stateVersion := env("NODE_STATUS_STATE_VERSION", "unknown")
	repo := env("NODE_STATUS_COMIN_REPO", "/var/lib/comin/source/repository")
	currentSystem := env("NODE_STATUS_CURRENT_SYSTEM", "/run/current-system")
	profilesSystem := env("NODE_STATUS_PROFILES_SYSTEM", "/nix/var/nix/profiles/system")
	currentMetadata := func() metadata {
		return readMetadata(stateVersion, repo, currentSystem, profilesSystem)
	}
	unitList, configured := os.LookupEnv("NODE_STATUS_SYSTEMD_UNITS")
	if !configured {
		unitList = "caddy.service comin.service llm-gateway.service prometheus.service loki.service alloy.service grafana.service rns-server.service rnsh.service"
	}
	units := strings.Fields(unitList)
	c := &collector{procRoot: env("NODE_STATUS_PROC_ROOT", "/proc"), sysRoot: env("NODE_STATUS_SYS_ROOT", "/sys"), rootMount: env("NODE_STATUS_ROOT_MOUNT", "/"), systemctl: env("NODE_STATUS_SYSTEMCTL", "systemctl"), units: units}
	_, _, _ = c.cpu()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(statusResponse{currentMetadata(), c.snapshot()})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "{\"status\":\"ok\"}\n")
	})
	mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		c.metrics(w, currentMetadata(), started)
	})
	srv := &http.Server{Addr: *listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	slog.Info("node-status listening", "address", *listen, "version", version)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("node-status stopped", "error", err)
		os.Exit(1)
	}
}
