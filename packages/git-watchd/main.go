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

	"github.com/mytecor/meshbus/security/realm"
	meshrns "github.com/mytecor/meshbus/transport/rns"
)

var version = "0.1.0"

func main() {
	var (
		realmKeyFile  string
		realmKeyHex   string
		identityPath  string
		allowedDir    string
		gitBin        string
		headSubject   string
		watchSubject  string
		pollInterval  time.Duration
		showVersion   bool
	)

	flag.StringVar(&realmKeyFile, "realm-key-file", os.Getenv("MESHBUS_REALM_KEY_FILE"), "Path to 32-byte realm key file")
	flag.StringVar(&realmKeyHex, "realm-key", os.Getenv("MESHBUS_REALM_KEY"), "32-byte realm key as hex string")
	flag.StringVar(&identityPath, "identity-file", os.Getenv("MESHBUS_IDENTITY"), "Path to RNS identity file")
	flag.StringVar(&allowedDir, "allowed-dir", os.Getenv("GIT_WATCHD_ALLOWED_DIR"), "Allowed directory prefix for local git repositories")
	flag.StringVar(&gitBin, "git-bin", "git", "Path to git binary")
	flag.StringVar(&headSubject, "head-subject", DefaultSubjectHead, "Meshbus subject for git.head")
	flag.StringVar(&watchSubject, "watch-subject", DefaultSubjectWatch, "Meshbus subject for git.watch")
	flag.DurationVar(&pollInterval, "poll-interval", DefaultPollInterval, "Polling interval for git ref checking")
	flag.BoolVar(&showVersion, "version", false, "Print version and exit")
	flag.Parse()

	if showVersion {
		fmt.Printf("git-watchd %s\n", version)
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
		identityPath = "git-watchd.identity"
	}

	node, err := meshrns.NewNode(meshrns.NodeConfig{
		Endpoint: meshrns.Config{
			StackMode:      meshrns.StackSharedClient,
			IdentitySource: identityPath,
			RealmKey:       realmKey,
			PresenceMetadata: map[string]string{
				"service": "git-watchd",
			},
		},
	})
	if err != nil {
		log.Fatalf("failed to create meshbus node: %v", err)
	}
	defer node.Close()

	engine := NewGoGitEngine(allowedDir)
	watcher := NewWatcher(engine, pollInterval)

	server, err := NewServer(ServerConfig{
		Bus:          node,
		Engine:       engine,
		Watcher:      watcher,
		HeadSubject:  headSubject,
		WatchSubject: watchSubject,
	})
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}
	defer server.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := node.Start(ctx); err != nil {
		log.Fatalf("failed to start meshbus node: %v", err)
	}
	if err := server.Start(ctx); err != nil {
		log.Fatalf("failed to start git-watchd server: %v", err)
	}

	log.Printf("git-watchd %s started (meshbus peer %s, subjects %s, %s)",
		version, node.Identity(), headSubject, watchSubject)

	<-ctx.Done()
	log.Println("git-watchd shutting down...")
}
