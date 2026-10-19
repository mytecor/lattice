package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/mytecor/lattice/agentd/internal/acp"
	"github.com/mytecor/lattice/agentd/internal/loop"
	"github.com/mytecor/lattice/agentd/internal/runtime"
	"github.com/mytecor/lattice/agentd/internal/verifier"
	taskcontract "github.com/mytecor/lattice/task-contract"
	"github.com/mytecor/meshbus/core"
	"github.com/mytecor/meshbus/security/realm"
	meshrns "github.com/mytecor/meshbus/transport/rns"
)

var version = "0.2.0"

func main() {
	var (
		realmKeyFile  string
		realmKeyHex   string
		identityPath  string
		agentSubject  string
		workDir       string
		maxIterations int
		showVersion   bool
	)

	flag.StringVar(&realmKeyFile, "realm-key-file", os.Getenv("MESHBUS_REALM_KEY_FILE"), "Path to 32-byte realm key file")
	flag.StringVar(&realmKeyHex, "realm-key", os.Getenv("MESHBUS_REALM_KEY"), "32-byte realm key as hex string")
	flag.StringVar(&identityPath, "identity-file", os.Getenv("MESHBUS_IDENTITY"), "Path to RNS identity file")
	flag.StringVar(&agentSubject, "agent-subject", taskcontract.DefaultSubjectAgentRun, "Meshbus subject for agent.run")
	flag.StringVar(&workDir, "work-dir", os.Getenv("AGENTD_WORK_DIR"), "Directory for temporary execution worktrees")
	flag.IntVar(&maxIterations, "max-iterations", 5, "Default maximum iterations for agent execution loop")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
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
	rt := runtime.NewDummyRuntime(workDir)
	acpClient := acp.NewDummyClient()
	ver := verifier.NewCommandVerifier()
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
