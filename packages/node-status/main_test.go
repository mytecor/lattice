package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseCPU(t *testing.T) {
	c, err := parseCPU(strings.NewReader("cpu  100 2 30 400 5 6 7 8 0 0\ncpu0 1 2 3 4\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c.User <= 0 || c.Idle <= c.User {
		t.Fatalf("unexpected CPU counters: %+v", c)
	}
}

func TestParseMeminfo(t *testing.T) {
	m, err := parseMeminfo(strings.NewReader("MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 50 kB\nSwapFree: 20 kB\n"))
	if err != nil {
		t.Fatal(err)
	}
	if m.Total != 1024000 || m.Available != 256000 || m.SwapFree != 20480 {
		t.Fatalf("unexpected memory: %+v", m)
	}
}

func TestParseNetdevExcludesLoopback(t *testing.T) {
	n := parseNetdev(strings.NewReader("Inter-| Receive | Transmit\n lo: 1 2 3 4 0 0 0 0 5 6 7 8 0 0 0 0\n eth0: 10 11 12 13 0 0 0 0 20 21 22 23 0 0 0 0\n"))
	if _, ok := n["lo"]; ok {
		t.Fatal("loopback must not be exported")
	}
	if n["eth0"].TransmitDrops != 23 || n["eth0"].ReceiveErrors != 12 {
		t.Fatalf("unexpected network stats: %+v", n)
	}
}

func TestParseDiskStat(t *testing.T) {
	d, err := parseDiskStat("10 0 20 0 30 0 40 0 0 500 0")
	if err != nil {
		t.Fatal(err)
	}
	if d.Reads != 10 || d.Writes != 30 || d.IOTimeMS != 500 {
		t.Fatalf("unexpected disk stats: %+v", d)
	}
}

func TestMetricsContract(t *testing.T) {
	root := t.TempDir()
	write := func(name, value string) {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("proc/stat", "cpu 100 2 30 400 5 6 7 8 0 0\n")
	write("proc/meminfo", "MemTotal: 1000 kB\nMemAvailable: 250 kB\nSwapTotal: 50 kB\nSwapFree: 20 kB\n")
	write("proc/uptime", "123.5 0\n")
	write("proc/loadavg", "0.1 0.2 0.3 1/10 1\n")
	write("proc/net/dev", "eth0: 10 11 12 13 0 0 0 0 20 21 22 23 0 0 0 0\n")
	write("sys/block/sda/stat", "10 0 20 0 30 0 40 0 0 500 0\n")
	write("sys/class/thermal/thermal_zone0/type", "cpu\n")
	write("sys/class/thermal/thermal_zone0/temp", "42500\n")

	c := &collector{procRoot: filepath.Join(root, "proc"), sysRoot: filepath.Join(root, "sys"), rootMount: root}
	_, _, _ = c.cpu()
	write("proc/stat", "cpu 110 2 35 405 5 6 7 8 0 0\n")
	var out bytes.Buffer
	c.metrics(&out, metadata{Node: "node-a", StateVersion: "26.05", Kernel: "test"}, time.Unix(100, 0))
	text := out.String()
	for _, want := range []string{
		"node_status_up 1",
		"node_status_cpu_seconds_total{mode=\"user\"}",
		"node_status_memory_total_bytes 1.024e+06",
		"node_status_network_receive_bytes_total{device=\"eth0\"} 10",
		"node_status_disk_read_bytes_total{device=\"sda\"} 10240",
		"node_status_thermal_zone_celsius{type=\"cpu\"} 42.5",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("metrics missing %q:\n%s", want, text)
		}
	}
}

func TestReadMetadataFollowsCurrentGenerationAndCommit(t *testing.T) {
	root := t.TempDir()
	profiles := filepath.Join(root, "profiles-system")
	current := filepath.Join(root, "current-system")
	repo := filepath.Join(root, "repository")
	if err := os.MkdirAll(filepath.Join(repo, "refs/lattice"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("system-42-link", profiles); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/nix/store/mock-system", current); err != nil {
		t.Fatal(err)
	}
	first := strings.Repeat("a", 40)
	if err := os.WriteFile(filepath.Join(repo, "refs/lattice/source"), []byte(first+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := readMetadata("26.05", repo, current, profiles)
	if m.Generation == nil || *m.Generation != 42 {
		t.Fatalf("generation = %v", m.Generation)
	}
	if m.Commit == nil || *m.Commit != first {
		t.Fatalf("commit = %v", m.Commit)
	}

	second := strings.Repeat("b", 40)
	if err := os.WriteFile(filepath.Join(repo, "refs/lattice/source"), []byte(second+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m = readMetadata("26.05", repo, current, profiles)
	if m.Commit == nil || *m.Commit != second {
		t.Fatalf("updated commit = %v", m.Commit)
	}
}
