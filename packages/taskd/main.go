package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	taskcontract "github.com/mytecor/lattice/task-contract"
	"github.com/mytecor/meshbus/security/realm"
	meshrns "github.com/mytecor/meshbus/transport/rns"
)

var version = "0.1.0"

func main() {
	var (
		realmKeyFile string
		realmKeyHex  string
		identityPath string
		taskRepo     string
		ref          string
		headSubject  string
		watchSubject string
		agentSubject string
		watchTimeout time.Duration
		showVersion  bool
	)

	flag.StringVar(&realmKeyFile, "realm-key-file", os.Getenv("MESHBUS_REALM_KEY_FILE"), "Path to 32-byte realm key file")
	flag.StringVar(&realmKeyHex, "realm-key", os.Getenv("MESHBUS_REALM_KEY"), "32-byte realm key as hex string")
	flag.StringVar(&identityPath, "identity-file", os.Getenv("MESHBUS_IDENTITY"), "Path to RNS identity file")
	flag.StringVar(&taskRepo, "task-repo", os.Getenv("TASKD_TASK_REPO"), "Path to local Git task repository")
	flag.StringVar(&ref, "ref", "HEAD", "Git ref to reconcile")
	flag.StringVar(&headSubject, "head-subject", DefaultSubjectHead, "Meshbus subject for git.head")
	flag.StringVar(&watchSubject, "watch-subject", DefaultSubjectWatch, "Meshbus subject for git.watch")
	flag.StringVar(&agentSubject, "agent-subject", taskcontract.DefaultSubjectAgentRun, "Meshbus subject for agent.run")
	flag.DurationVar(&watchTimeout, "watch-timeout", 10*time.Second, "Timeout for git.watch long-poll")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("taskd %s\n", version)
		os.Exit(0)
	}

	if taskRepo == "" {
		log.Fatal("--task-repo is required")
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
		identityPath = "taskd.identity"
	}

	node, err := meshrns.NewNode(meshrns.NodeConfig{
		Endpoint: meshrns.Config{
			StackMode:      meshrns.StackSharedClient,
			IdentitySource: identityPath,
			RealmKey:       realmKey,
			PresenceMetadata: map[string]string{
				"service": "taskd",
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to create meshbus node: %v", err)
	}
	defer node.Close()

	gitWatcher := NewMeshbusGitWatcher(node, headSubject, watchSubject)
	gitReader := NewGoGitReader()
	agentCaller := NewMeshbusAgentCaller(node, agentSubject)

	reconciler, err := NewReconciler(ReconcilerConfig{
		RepoPath:     taskRepo,
		Ref:          ref,
		GitWatcher:   gitWatcher,
		GitReader:    gitReader,
		AgentCaller:  agentCaller,
		WatchTimeout: watchTimeout,
	})
	if err != nil {
		log.Fatalf("failed to create reconciler: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := node.Start(ctx); err != nil {
		log.Fatalf("failed to start meshbus node: %v", err)
	}

	log.Printf("taskd %s started (meshbus peer %s, task-repo %s, ref %s)",
		version, node.Identity(), taskRepo, ref)

	errCh := make(chan error, 1)
	go func() {
		errCh <- reconciler.Run(ctx)
	}()

	select {
	case <-ctx.Done():
		log.Println("taskd shutting down...")
		reconciler.Stop()
	case err := <-errCh:
		if err != nil && !os.IsTimeout(err) {
			log.Fatalf("taskd reconciler failed: %v", err)
		}
	}
}
