package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	taskcontract "github.com/mytecor/lattice/task-contract"
)

// ResultHandler is an optional hook invoked when a terminal TaskResult is produced by agentd.
// In f11-05, this publishes the result back into Git.
type ResultHandler func(ctx context.Context, spec *taskcontract.TaskSpec, result *taskcontract.TaskResult) error

// ReconcilerConfig holds configuration for the Reconciler.
type ReconcilerConfig struct {
	RepoPath      string
	Ref           string
	GitWatcher    GitWatcher
	GitReader     GitReader
	AgentCaller   AgentCaller
	WatchTimeout  time.Duration
	ResultHandler ResultHandler
}

// Reconciler reconciles Git task specifications into actionable TaskSpec executions.
// It maintains only ephemeral, lossy in-memory state mapping: task revision -> running request.
type Reconciler struct {
	repoPath      string
	ref           string
	gitWatcher    GitWatcher
	gitReader     GitReader
	agentCaller   AgentCaller
	watchTimeout  time.Duration
	resultHandler ResultHandler

	mu          sync.Mutex
	activeRuns  map[string]context.CancelFunc // revisionKey -> cancelFunc
	activePaths map[string]string             // path -> revisionKey
	wg          sync.WaitGroup
}

// NewReconciler creates a new Reconciler.
func NewReconciler(cfg ReconcilerConfig) (*Reconciler, error) {
	if cfg.RepoPath == "" {
		return nil, errors.New("repoPath is required")
	}
	if cfg.Ref == "" {
		cfg.Ref = "HEAD"
	}
	if cfg.GitWatcher == nil {
		return nil, errors.New("gitWatcher is required")
	}
	if cfg.GitReader == nil {
		cfg.GitReader = NewGoGitReader()
	}
	if cfg.AgentCaller == nil {
		return nil, errors.New("agentCaller is required")
	}
	if cfg.WatchTimeout <= 0 {
		cfg.WatchTimeout = 10 * time.Second
	}

	return &Reconciler{
		repoPath:      cfg.RepoPath,
		ref:           cfg.Ref,
		gitWatcher:    cfg.GitWatcher,
		gitReader:     cfg.GitReader,
		agentCaller:   cfg.AgentCaller,
		watchTimeout:  cfg.WatchTimeout,
		resultHandler: cfg.ResultHandler,
		activeRuns:    make(map[string]context.CancelFunc),
		activePaths:   make(map[string]string),
	}, nil
}

// ActiveRunsCount returns the number of currently executing task revisions.
func (r *Reconciler) ActiveRunsCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.activeRuns)
}

// IsRunning checks whether a specific task revision is currently running.
func (r *Reconciler) IsRunning(revisionKey string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.activeRuns[revisionKey]
	return ok
}

// dispatch starts execution of an actionable TaskSpec if not already running.
func (r *Reconciler) dispatch(parentCtx context.Context, spec *taskcontract.TaskSpec) bool {
	if spec == nil || !spec.IsActionable() {
		return false
	}

	revKey := spec.RevisionKey()

	r.mu.Lock()
	if _, exists := r.activeRuns[revKey]; exists {
		// Already executing this exact immutable revision
		r.mu.Unlock()
		log.Printf("taskd: revision %s already running, skipping duplicate dispatch", revKey)
		return false
	}

	// If an older revision of the same path is running, cancel it
	if oldRevKey, running := r.activePaths[spec.Path]; running && oldRevKey != revKey {
		if cancelOld, ok := r.activeRuns[oldRevKey]; ok {
			log.Printf("taskd: superseding older revision %s for path %s with new revision %s",
				oldRevKey, spec.Path, revKey)
			cancelOld()
			delete(r.activeRuns, oldRevKey)
		}
	}

	runCtx, cancel := context.WithCancel(parentCtx)
	r.activeRuns[revKey] = cancel
	r.activePaths[spec.Path] = revKey
	r.mu.Unlock()

	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.activeRuns, revKey)
			if r.activePaths[spec.Path] == revKey {
				delete(r.activePaths, spec.Path)
			}
			r.mu.Unlock()
		}()

		log.Printf("taskd: executing task %s (revision %s)", spec.ID, revKey)
		result, err := r.agentCaller.RunTask(runCtx, *spec)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				log.Printf("taskd: task %s (revision %s) canceled", spec.ID, revKey)
			} else {
				log.Printf("taskd: task %s (revision %s) execution error: %v", spec.ID, revKey, err)
			}
			return
		}

		if result != nil {
			log.Printf("taskd: task %s (revision %s) finished with status %s",
				spec.ID, revKey, result.Status)
			if r.resultHandler != nil {
				if hErr := r.resultHandler(runCtx, spec, result); hErr != nil {
					log.Printf("taskd: result handler error for task %s: %v", spec.ID, hErr)
				}
			}
		}
	}()

	return true
}

// Run performs startup reconciliation of all actionable tasks at the current Git HEAD,
// and then continuously processes revision changes from git-watchd.
func (r *Reconciler) Run(ctx context.Context) error {
	// 1. Startup phase: resolve current HEAD from git-watchd
	headRev, err := r.gitWatcher.Head(ctx, r.repoPath, r.ref)
	if err != nil {
		return fmt.Errorf("taskd startup: failed to get head for %s (%s): %w", r.repoPath, r.ref, err)
	}
	log.Printf("taskd: startup HEAD resolved to %s for %s (%s)", headRev, r.repoPath, r.ref)

	// 2. Scan task repository at startup HEAD and reconcile all actionable revisions
	tasks, err := r.gitReader.ScanTasksAtCommit(ctx, r.repoPath, headRev)
	if err != nil {
		return fmt.Errorf("taskd startup: failed to scan tasks at %s: %w", headRev, err)
	}

	actionableCount := 0
	for _, spec := range tasks {
		if spec.IsActionable() {
			actionableCount++
			log.Printf("taskd: startup scan found actionable task %s at %s", spec.ID, spec.RevisionKey())
			r.dispatch(ctx, spec)
		} else {
			log.Printf("taskd: task %s at %s is terminal (%s), skipping",
				spec.ID, spec.RevisionKey(), spec.Result.Status)
		}
	}
	log.Printf("taskd: startup scan complete (%d total tasks, %d actionable dispatched)",
		len(tasks), actionableCount)

	// 3. Watch loop: continuously call git.watch from cursor
	cursor := headRev
	for {
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return ctx.Err()
		default:
		}

		watchResp, err := r.gitWatcher.Watch(ctx, r.repoPath, r.ref, cursor, r.watchTimeout)
		if err != nil {
			if ctx.Err() != nil {
				r.wg.Wait()
				return ctx.Err()
			}
			log.Printf("taskd: git.watch error: %v, retrying after backoff", err)
			select {
			case <-ctx.Done():
				r.wg.Wait()
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}

			// In case cursor became invalid (e.g. rebase / force-push), re-query HEAD
			if newHead, hErr := r.gitWatcher.Head(ctx, r.repoPath, r.ref); hErr == nil && newHead != cursor {
				log.Printf("taskd: resetting watch cursor to new HEAD %s", newHead)
				cursor = newHead
			}
			continue
		}

		switch watchResp.Status {
		case StatusNoChange:
			// No change during watch timeout; re-issue watch immediately with same cursor
			continue

		case StatusChanged:
			log.Printf("taskd: detected revision change %s -> %s (%d changed paths)",
				watchResp.From, watchResp.To, len(watchResp.ChangedPaths))
			cursor = watchResp.To

			for _, path := range watchResp.ChangedPaths {
				if !isTaskPath(path) {
					continue
				}

				spec, err := r.gitReader.ReadTaskAtCommit(ctx, r.repoPath, path, watchResp.To)
				if err != nil {
					// File may have been deleted or invalid format
					log.Printf("taskd: path %s at %s: %v", path, watchResp.To, err)
					continue
				}

				if spec.IsActionable() {
					log.Printf("taskd: changed task %s at %s is actionable, dispatching", spec.ID, spec.RevisionKey())
					r.dispatch(ctx, spec)
				} else {
					log.Printf("taskd: changed task %s at %s is terminal (%s), ignoring",
						spec.ID, spec.RevisionKey(), spec.Result.Status)
				}
			}

		case StatusError:
			log.Printf("taskd: git-watchd reported error: %s", watchResp.Error)
			select {
			case <-ctx.Done():
				r.wg.Wait()
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}
		}
	}
}

// Stop waits for all running tasks to complete.
func (r *Reconciler) Stop() {
	r.mu.Lock()
	for _, cancel := range r.activeRuns {
		cancel()
	}
	r.mu.Unlock()
	r.wg.Wait()
}
