package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	r1s "github.com/mytecor/r1s/client"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/mytecor/lattice/agentd/internal/acp"
	"github.com/mytecor/lattice/agentd/internal/loop"
	"github.com/mytecor/lattice/agentd/internal/runtime"
	"github.com/mytecor/lattice/agentd/internal/verifier"
	taskcontract "github.com/mytecor/lattice/task-contract"
	"github.com/mytecor/meshbus/core"
	"github.com/mytecor/meshbus/security/realm"
	meshrns "github.com/mytecor/meshbus/transport/rns"
)

var version = "0.3.0"

func main() {
	var (
		realmKeyFile                                                                    string
		realmKeyHex                                                                     string
		identityPath                                                                    string
		agentSubject                                                                    string
		workDir                                                                         string
		maxIterations                                                                   int
		showVersion                                                                     bool
		backend, clusterKeyFile, bootstrap, tunnelPeers, environmentFile, resourceClass string
		acpPort                                                                         uint
		offerWait                                                                       time.Duration
	)

	flag.StringVar(&realmKeyFile, "realm-key-file", os.Getenv("MESHBUS_REALM_KEY_FILE"), "Path to 32-byte realm key file")
	flag.StringVar(&realmKeyHex, "realm-key", os.Getenv("MESHBUS_REALM_KEY"), "32-byte realm key as hex string")
	flag.StringVar(&identityPath, "identity-file", os.Getenv("MESHBUS_IDENTITY"), "Path to RNS identity file")
	flag.StringVar(&agentSubject, "agent-subject", taskcontract.DefaultSubjectAgentRun, "Meshbus subject for agent.run")
	flag.StringVar(&workDir, "work-dir", os.Getenv("AGENTD_WORK_DIR"), "Directory for temporary execution worktrees")
	flag.IntVar(&maxIterations, "max-iterations", 5, "Default maximum iterations for agent execution loop")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.StringVar(&backend, "runtime", "dummy", "Execution runtime: dummy or r1s")
	flag.StringVar(&clusterKeyFile, "r1s-cluster-key-file", "", "Path to raw 32-byte or hex r1s cluster key (separate from Meshbus realm)")
	flag.StringVar(&bootstrap, "r1s-bootstrap-destinations", "", "Comma-separated public allocator destinations")
	flag.StringVar(&tunnelPeers, "r1s-tunnel-peers", "", "Comma-separated tunnel bootstrap peer URIs")
	flag.StringVar(&environmentFile, "agent-environment-file", "", "Runtime JSON object with scoped agent environment (never logged)")
	flag.StringVar(&resourceClass, "r1s-resource-class", "default", "r1s resource class")
	flag.UintVar(&acpPort, "acp-port", 55514, "Container ACP port")
	flag.DurationVar(&offerWait, "r1s-offer-wait", 10*time.Second, "Allocator offer collection window")
	flag.Parse()

	if showVersion {
		fmt.Printf("agentd %s\n", version)
		os.Exit(0)
	}

	var realmKey []byte
	if realmKeyFile != "" {
		data, err := os.ReadFile(realmKeyFile)
		if err != nil {
			log.Fatalf("failed to read realm key file %s: %v", realmKeyFile, err)
		}
		trimmed := strings.TrimSpace(string(data))
		if len(data) == realm.KeySize {
			realmKey = data
		} else if decoded, err := hex.DecodeString(trimmed); err == nil && len(decoded) == realm.KeySize {
			realmKey = decoded
		} else {
			log.Fatalf("realm key in %s must be %d raw bytes or 64 hex characters", realmKeyFile, realm.KeySize)
		}
	} else if realmKeyHex != "" {
		decoded, err := hex.DecodeString(strings.TrimSpace(realmKeyHex))
		if err != nil || len(decoded) != realm.KeySize {
			log.Fatalf("MESHBUS_REALM_KEY must contain %d bytes as hex", realm.KeySize)
		}
		realmKey = decoded
	} else {
		log.Fatal("either --realm-key-file or --realm-key must be provided")
	}

	if identityPath == "" {
		identityPath = "agentd.identity"
	}

	if workDir == "" {
		workDir = os.TempDir()
	}

	node, err := meshrns.NewNode(meshrns.NodeConfig{
		Endpoint: meshrns.Config{
			StackMode:      meshrns.StackSharedClient,
			IdentitySource: identityPath,
			RealmKey:       realmKey,
			PresenceMetadata: map[string]string{
				"service": "agentd",
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to create meshbus node: %v", err)
	}
	defer node.Close()

	bus := &nodeBus{node: node}
	var rt runtime.Runtime = runtime.NewDummyRuntime(workDir)
	var acpClient acp.Client = acp.NewDummyClient()
	var ver verifier.Verifier = verifier.NewCommandVerifier()
	switch backend {
	case "dummy":
	case "r1s":
		if acpPort == 0 || acpPort > 65535 {
			log.Fatal("acp-port must be in 1..65535")
		}
		key, err := readClusterKey(clusterKeyFile)
		if err != nil {
			log.Fatal(err)
		}
		environment := map[string]string{}
		if environmentFile != "" {
			data, err := os.ReadFile(environmentFile)
			if err != nil {
				log.Fatal("cannot read agent environment file")
			}
			if json.Unmarshal(data, &environment) != nil {
				log.Fatal("agent environment file must be a JSON string map")
			}
		}
		remote, err := runtime.NewR1SRuntime(runtime.R1SConfig{
			Client:  r1s.Config{ClusterKey: key, BootstrapDestinations: splitList(bootstrap), TunnelPeers: splitList(tunnelPeers)},
			ACPPort: uint16(acpPort), ResourceClass: resourceClass, Environment: environment, OfferWait: offerWait,
		})
		if err != nil {
			log.Fatalf("configure r1s runtime: %v", err)
		}
		rt = remote
		acpClient = &acp.WebSocketClient{Dial: remote.DialACP}
		ver = verifier.PendingRemote{}
	default:
		log.Fatal("runtime must be dummy or r1s")
	}
	execLoop, err := loop.New(loop.Config{
		Runtime:              rt,
		ACPClient:            acpClient,
		Verifier:             ver,
		DefaultMaxIterations: maxIterations,
	})
	if err != nil {
		log.Fatalf("failed to create execution loop: %v", err)
	}

	server, err := NewServer(ServerConfig{
		Bus:          bus,
		AgentSubject: agentSubject,
		Executor:     execLoop,
	})
	if err != nil {
		log.Fatalf("failed to create agentd server: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := node.Start(ctx); err != nil {
		log.Fatalf("failed to start meshbus node: %v", err)
	}

	log.Printf("agentd %s started (meshbus peer %s, subject %s, workDir %s, maxIterations %d)",
		version, node.Identity(), agentSubject, workDir, maxIterations)

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.Run(ctx)
	}()

	select {
	case <-ctx.Done():
		log.Println("agentd shutting down...")
		server.Stop()
	case err := <-errCh:
		if err != nil && !os.IsTimeout(err) {
			log.Fatalf("agentd server failed: %v", err)
		}
	}
}

type nodeBus struct {
	node *core.Node
}

func (b *nodeBus) Subscribe(pattern string, handler func(ctx context.Context, event core.ReceivedEvent) error) (io.Closer, error) {
	return b.node.Subscribe(pattern, handler)
}

func (b *nodeBus) Publish(ctx context.Context, topic string, payload []byte, opts core.PublishOptions) error {
	_, err := b.node.Publish(ctx, topic, payload, opts)
	return err
}

func splitList(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
}

func readClusterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read r1s cluster key file")
	}
	if len(data) == 32 {
		return data, nil
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(data)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("r1s cluster key must be 32 raw bytes or 64 hex characters")
	}
	return key, nil
}
