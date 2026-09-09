package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Printf("llm-gateway: %v", err)
		os.Exit(1)
	}
}

func run(arguments []string) error {
	flags := flag.NewFlagSet("llm-gateway", flag.ContinueOnError)
	configPath := flags.String("config", "", "path to runtime JSON config")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *configPath == "" || flags.NArg() != 1 || flags.Arg(0) != "serve" {
		return errors.New("usage: llm-gateway --config PATH serve")
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	compiled, err := compileConfig(cfg)
	if err != nil {
		return fmt.Errorf("validate config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	catalog := newCatalog(compiled)
	reportCatalogErrors(compiled.logger, catalog.Refresh(ctx))
	catalog.Start(ctx, compiled.raw.CatalogRefreshInterval.Duration, func(errorsByGroup map[string]error) {
		reportCatalogErrors(compiled.logger, errorsByGroup)
	})
	executor, err := newBifrostExecutor(ctx, compiled)
	if err != nil {
		return err
	}
	defer executor.Close()
	runner := newRunner(compiled, catalog, executor)
	defer runner.Close()
	httpServer := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", compiled.raw.Host, compiled.raw.Port),
		Handler:           newServer(compiled, catalog, runner),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	compiled.logger.Info("gateway starting",
		"address", httpServer.Addr,
		"log_level", compiled.raw.LogLevel,
		"providers", len(compiled.providers),
		"models", len(compiled.logicalIDs),
	)

	serveErrors := make(chan error, 1)
	go func() { serveErrors <- httpServer.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	case err := <-serveErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func reportCatalogErrors(logger *slog.Logger, errorsByProvider map[string]error) {
	for providerID, err := range errorsByProvider {
		logger.Warn("catalog refresh failed", "provider", providerID, "detail", safeLogDetail(err.Error()))
	}
}
