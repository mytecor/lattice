package runtime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/mytecor/lattice/agentd/internal/acp"
	taskcontract "github.com/mytecor/lattice/task-contract"
	r1sv1 "github.com/mytecor/r1s/api/gen/r1s/v1"
	r1s "github.com/mytecor/r1s/client"
)

type runClient interface {
	Start(context.Context) error
	Run(context.Context, *r1sv1.ExecutionRequest, r1s.RunOptions) (r1s.Result, error)
	Close() []r1s.PendingRelease
}
type runTunnel interface {
	SetActive(string)
	Dial(context.Context, uint16) (r1s.Stream, error)
	Close() error
}

type R1SConfig struct {
	Client        r1s.Config
	ACPPort       uint16
	ResourceClass string
	Environment   map[string]string
	OfferWait     time.Duration
}

// R1SRuntime owns only ephemeral per-request clients, leases and tunnels.
type R1SRuntime struct {
	cfg       R1SConfig
	mu        sync.Mutex
	instances map[string]*r1sInstance
	create    func() (runClient, runTunnel, error)
}

func NewR1SRuntime(cfg R1SConfig) (*R1SRuntime, error) {
	if len(cfg.Client.ClusterKey) != 32 {
		return nil, errors.New("r1s cluster key must contain 32 bytes")
	}
	if cfg.ACPPort == 0 {
		cfg.ACPPort = 55514
	}
	if cfg.ResourceClass == "" {
		cfg.ResourceClass = "default"
	}
	if cfg.OfferWait < 0 {
		return nil, errors.New("r1s offer wait must be positive")
	}
	cfg.Environment = maps.Clone(cfg.Environment)
	r := &R1SRuntime{cfg: cfg, instances: make(map[string]*r1sInstance)}
	r.create = func() (runClient, runTunnel, error) {
		c, err := r1s.New(cfg.Client)
		if err != nil {
			return nil, nil, err
		}
		t, err := c.NewRunTunnel([]r1s.TunnelTarget{{Port: cfg.ACPPort}})
		if err != nil {
			c.Close()
			return nil, nil, err
		}
		return c, t, nil
	}
	return r, nil
}

type r1sInstance struct {
	owner           *R1SRuntime
	endpoint, token string
	client          runClient
	tunnel          runTunnel
	cancel          context.CancelFunc
	done            chan struct{}
	mu              sync.Mutex
	active          string
	changed         chan struct{}
	ended           bool
	runErr          error
	once            sync.Once
}

func (i *r1sInstance) Endpoint() string { return i.endpoint }
func (i *r1sInstance) WorkDir() string  { return "" }
func (i *r1sInstance) Commit() string   { return "" }
func (i *r1sInstance) SetCommit(string) {}
func (i *r1sInstance) update(active string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.ended {
		return
	}
	// Invalidate readers before SetActive closes the old streams: ACP can then
	// distinguish reassignment from an ordinary socket error and avoid replay.
	close(i.changed)
	i.changed = make(chan struct{})
	i.active = active
	i.tunnel.SetActive(active)
}
func (i *r1sInstance) Stop(ctx context.Context) error {
	i.once.Do(func() {
		i.owner.mu.Lock()
		delete(i.owner.instances, i.endpoint)
		i.owner.mu.Unlock()
		i.cancel()
	})
	select {
	case <-i.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *R1SRuntime) Start(ctx context.Context, spec *taskcontract.TaskSpec) (Instance, error) {
	if spec == nil || spec.Runtime.Image == "" || spec.Source.Repo == "" || spec.Source.Revision == "" {
		return nil, errors.New("r1s requires image, source repository and source revision")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c, t, err := r.create()
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	if err = c.Start(runCtx); err != nil {
		cancel()
		_ = t.Close()
		c.Close()
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err = rand.Read(tokenBytes); err != nil {
		cancel()
		_ = t.Close()
		c.Close()
		return nil, err
	}
	endpointBytes := make([]byte, 16)
	if _, err = rand.Read(endpointBytes); err != nil {
		cancel()
		_ = t.Close()
		c.Close()
		return nil, err
	}
	i := &r1sInstance{owner: r, endpoint: "r1s://" + hex.EncodeToString(endpointBytes), token: hex.EncodeToString(tokenBytes), client: c, tunnel: t, cancel: cancel, done: make(chan struct{}), changed: make(chan struct{})}
	env := maps.Clone(r.cfg.Environment)
	if env == nil {
		env = map[string]string{}
	}
	env["SOURCE_REPO"] = spec.Source.Repo
	env["SOURCE_REVISION"] = spec.Source.Revision
	env["WORKSPACE_DIR"] = "/workspace"
	env["ACP_PORT"] = strconv.Itoa(int(r.cfg.ACPPort))
	env["ACP_SECRET"] = i.token
	request := &r1sv1.ExecutionRequest{Workload: &r1sv1.Workload{Image: spec.Runtime.Image, Environment: env}, Policy: &r1sv1.ExecutionPolicy{}, ResourceClass: r.cfg.ResourceClass}
	r.mu.Lock()
	r.instances[i.endpoint] = i
	r.mu.Unlock()
	go func() {
		defer close(i.done)
		defer c.Close()
		defer t.Close()
		_, runErr := c.Run(runCtx, request, r1s.RunOptions{OfferWait: r.cfg.OfferWait, OnEvent: func(e r1s.Event) {
			switch e.Kind {
			case r1s.EventAttemptAssigned:
				i.update(e.Attempt.ExecutionID)
			case r1s.EventLeaseLost:
				i.update("")
			}
		}})
		i.mu.Lock()
		i.ended = true
		i.runErr = runErr
		close(i.changed)
		i.mu.Unlock()
	}()
	return i, nil
}

// DialACP adapts the r1s stream with net.Pipe, whose deadlines allow the HTTP
// WebSocket handshake to be canceled without opening a host TCP listener.
func (r *R1SRuntime) DialACP(ctx context.Context, endpoint string) (acp.Transport, error) {
	r.mu.Lock()
	i := r.instances[endpoint]
	r.mu.Unlock()
	if i == nil {
		return acp.Transport{}, errors.New("unknown r1s endpoint")
	}
	for {
		i.mu.Lock()
		active, ch, ended, runErr := i.active, i.changed, i.ended, i.runErr
		i.mu.Unlock()
		if ended {
			if runErr != nil {
				return acp.Transport{}, fmt.Errorf("r1s run ended: %w", runErr)
			}
			return acp.Transport{}, errors.New("r1s workload exited before ACP completed")
		}
		if active == "" {
			select {
			case <-ctx.Done():
				return acp.Transport{}, ctx.Err()
			case <-ch:
				continue
			}
		}
		stream, err := i.tunnel.Dial(ctx, r.cfg.ACPPort)
		if err != nil {
			// Container bootstrap may still be cloning the source repository. Retry
			// only opening the stream, never a submitted ACP turn on the same execution.
			timer := time.NewTimer(200 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return acp.Transport{}, ctx.Err()
			case <-ch:
				timer.Stop()
			case <-timer.C:
			}
			continue
		}
		local, remote := net.Pipe()
		var once sync.Once
		closeBoth := func() { once.Do(func() { _ = remote.Close(); _ = stream.Close() }) }
		go func() { defer closeBoth(); _, _ = io.Copy(stream, remote) }()
		go func() { defer closeBoth(); _, _ = io.Copy(remote, stream) }()
		return acp.Transport{Conn: local, Changed: ch, Token: i.token}, nil
	}
}
