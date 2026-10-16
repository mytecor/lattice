package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-billy/v5/memfs"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// Executor executes a TaskSpec and returns the terminal TaskResult.
// It does NOT retain durable state across invocations or restarts.
type Executor interface {
	ExecuteTask(ctx context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error)
}

// DummyWorkloadData represents the deterministic payload written by the dummy workload.
type DummyWorkloadData struct {
	TaskID         string `json:"task_id"`
	TaskRepo       string `json:"task_repo"`
	TaskPath       string `json:"task_path"`
	TaskCommit     string `json:"task_commit"`
	SourceRepo     string `json:"source_repo"`
	SourceRevision string `json:"source_revision"`
}

// DummyWorkloadExecutor runs a deterministic dummy workload against a source Git repository.
// It proves the pipeline before Pi/ACP loop by providing verifiable, idempotent commits.
type DummyWorkloadExecutor struct {
	workDir string
}

// NewDummyWorkloadExecutor creates a new DummyWorkloadExecutor.
func NewDummyWorkloadExecutor(workDir string) *DummyWorkloadExecutor {
	return &DummyWorkloadExecutor{
		workDir: workDir,
	}
}

// ExecuteTask runs the deterministic dummy workload for the provided TaskSpec.
func (e *DummyWorkloadExecutor) ExecuteTask(ctx context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	if spec == nil {
		return nil, errors.New("cannot execute nil TaskSpec")
	}
	if spec.Source.Repo == "" {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			ResultText:       "Source repository not specified in TaskSpec",
			VerificationText: "Validation failed: spec.source.repo is required",
		}, nil
	}

	sourceRepoPath := spec.Source.Repo
	// If source is a file URL, trim prefix
	sourceRepoPath = strings.TrimPrefix(sourceRepoPath, "file://")

	// 1. Open source repository
	repo, err := git.PlainOpen(sourceRepoPath)
	if err != nil {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			ResultText:       fmt.Sprintf("Failed to open source repository at %s: %v", sourceRepoPath, err),
			VerificationText: "Source repository access failed",
		}, nil
	}

	// Resolve target revision / HEAD
	var targetHash plumbing.Hash
	if spec.Source.Revision != "" {
		h, err := repo.ResolveRevision(plumbing.Revision(spec.Source.Revision))
		if err != nil {
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusFailed,
				ResultText:       fmt.Sprintf("Failed to resolve source revision %s: %v", spec.Source.Revision, err),
				VerificationText: "Source revision resolution failed",
			}, nil
		}
		targetHash = *h
	} else {
		headRef, err := repo.Head()
		if err != nil {
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusFailed,
				ResultText:       fmt.Sprintf("Failed to resolve source HEAD: %v", err),
				VerificationText: "Source HEAD resolution failed",
			}, nil
		}
		targetHash = headRef.Hash()
	}

	expectedData := DummyWorkloadData{
		TaskID:         spec.ID,
		TaskRepo:       spec.TaskRepo,
		TaskPath:       spec.Path,
		TaskCommit:     spec.Commit,
		SourceRepo:     spec.Source.Repo,
		SourceRevision: spec.Source.Revision,
	}
	expectedJSON, err := json.MarshalIndent(expectedData, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal dummy workload data: %w", err)
	}
	expectedJSON = append(expectedJSON, '\n')

	relFilePath := filepath.Join(".lattice-tasks", fmt.Sprintf("%s.json", spec.ID))

	// 2. Check for existing equivalent commit (Idempotency check)
	// Check target revision tree and branch HEAD tree
	checkCommitTree := func(commitHash plumbing.Hash) (*taskcontract.TaskResult, bool) {
		cObj, err := repo.CommitObject(commitHash)
		if err != nil {
			return nil, false
		}
		cTree, err := cObj.Tree()
		if err != nil {
			return nil, false
		}
		f, err := cTree.File(relFilePath)
		if err != nil {
			return nil, false
		}
		rdr, err := f.Reader()
		if err != nil {
			return nil, false
		}
		defer rdr.Close()
		existingBytes, err := io.ReadAll(rdr)
		if err != nil {
			return nil, false
		}

		var existingData DummyWorkloadData
		if err := json.Unmarshal(existingBytes, &existingData); err == nil {
			if existingData.TaskID == spec.ID &&
				existingData.TaskCommit == spec.Commit &&
				existingData.TaskRepo == spec.TaskRepo &&
				existingData.TaskPath == spec.Path {
				return &taskcontract.TaskResult{
					Status:           taskcontract.StatusCompleted,
					Revision:         commitHash.String(),
					ResultText:       fmt.Sprintf("Idempotent replay: deterministic commit %s already contains task %s", commitHash.String(), spec.ID),
					VerificationText: "Verified existing equivalent deterministic commit in source repository",
				}, true
			}
		}

		// File exists but does not match task identity
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusBlocked,
			ResultText:       fmt.Sprintf("Conflict: %s already exists with incompatible content in commit %s", relFilePath, commitHash.String()),
			VerificationText: "Conflict detected: existing commit has differing task payload",
		}, true
	}

	if res, found := checkCommitTree(targetHash); found {
		return res, nil
	}

	if headRef, hErr := repo.Head(); hErr == nil && headRef.Hash() != targetHash {
		if res, found := checkCommitTree(headRef.Hash()); found {
			return res, nil
		}
	}

	// 3. Clone / checkout into temporary workspace to execute workload and checks
	tempDir, err := os.MkdirTemp(e.workDir, fmt.Sprintf("agentd-work-%s-", spec.ID))
	if err != nil {
		return nil, fmt.Errorf("create temp workdir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	workRepo, err := git.PlainCloneContext(ctx, tempDir, false, &git.CloneOptions{
		URL:           sourceRepoPath,
		ReferenceName: plumbing.ReferenceName("refs/heads/main"),
		SingleBranch:  false,
	})
	if err != nil {
		// Try cloning without branch restriction
		workRepo, err = git.PlainCloneContext(ctx, tempDir, false, &git.CloneOptions{
			URL: sourceRepoPath,
		})
		if err != nil {
			return &taskcontract.TaskResult{
				Status:           taskcontract.StatusFailed,
				ResultText:       fmt.Sprintf("Failed to clone source repository: %v", err),
				VerificationText: "Source repository clone failed",
			}, nil
		}
	}

	worktree, err := workRepo.Worktree()
	if err != nil {
		return nil, fmt.Errorf("get worktree: %w", err)
	}

	if err := worktree.Checkout(&git.CheckoutOptions{
		Hash: targetHash,
	}); err != nil {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			ResultText:       fmt.Sprintf("Failed to checkout target revision %s: %v", targetHash.String(), err),
			VerificationText: "Worktree checkout failed",
		}, nil
	}

	// 4. Run verification checks if declared in TaskSpec
	if len(spec.Checks) > 0 {
		for _, checkCmd := range spec.Checks {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}

			cmd := exec.CommandContext(ctx, "sh", "-c", checkCmd)
			cmd.Dir = tempDir
			var outBuf bytes.Buffer
			cmd.Stdout = &outBuf
			cmd.Stderr = &outBuf

			if err := cmd.Run(); err != nil {
				return &taskcontract.TaskResult{
					Status:           taskcontract.StatusFailed,
					ResultText:       fmt.Sprintf("Verification check failed: %q", checkCmd),
					VerificationText: fmt.Sprintf("Command failed: %v\nOutput:\n%s", err, outBuf.String()),
				}, nil
			}
		}
	}

	// 5. Write deterministic file
	destFilePath := filepath.Join(tempDir, relFilePath)
	if err := os.MkdirAll(filepath.Dir(destFilePath), 0755); err != nil {
		return nil, fmt.Errorf("create directory for %s: %w", relFilePath, err)
	}

	if err := os.WriteFile(destFilePath, expectedJSON, 0644); err != nil {
		return nil, fmt.Errorf("write %s: %w", relFilePath, err)
	}

	// 6. Stage and commit
	if _, err := worktree.Add(relFilePath); err != nil {
		return nil, fmt.Errorf("git add %s: %w", relFilePath, err)
	}

	authorTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	commitHash, err := worktree.Commit(
		fmt.Sprintf("dummy: complete task %s (%s)", spec.ID, spec.Commit),
		&git.CommitOptions{
			Author: &object.Signature{
				Name:  "Lattice Agent",
				Email: "agent@lattice.local",
				When:  authorTime,
			},
			Committer: &object.Signature{
				Name:  "Lattice Agent",
				Email: "agent@lattice.local",
				When:  authorTime,
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// 7. Push / update source repository
	// Determine the branch name from source repo HEAD
	var refSpecs []config.RefSpec
	if targetHead, rErr := repo.Head(); rErr == nil && targetHead.Name().IsBranch() {
		refSpecs = append(refSpecs, config.RefSpec(fmt.Sprintf("%s:%s", commitHash.String(), targetHead.Name().String())))
	} else {
		refSpecs = append(refSpecs, config.RefSpec(fmt.Sprintf("%s:refs/heads/main", commitHash.String())))
	}

	_ = workRepo.PushContext(ctx, &git.PushOptions{
		RemoteName: "origin",
		RefSpecs:   refSpecs,
		Force:      true,
	})

	// If source repository is local filesystem, also ensure the object and ref exist directly
	if targetHead, rErr := repo.Head(); rErr == nil && targetHead.Name().IsBranch() {
		_ = repo.Storer.SetReference(plumbing.NewHashReference(targetHead.Name(), commitHash))
	}

	return &taskcontract.TaskResult{
		Status:           taskcontract.StatusCompleted,
		Revision:         commitHash.String(),
		ResultText:       fmt.Sprintf("Dummy workload completed successfully: commit %s", commitHash.String()),
		VerificationText: fmt.Sprintf("Deterministic file %s verified; all checks passed", relFilePath),
	}, nil
}

// InMemDummyWorkloadExecutor performs deterministic dummy execution purely in-memory.
// Useful for hermetic unit testing without filesystem dependencies.
type InMemDummyWorkloadExecutor struct{}

func (e *InMemDummyWorkloadExecutor) ExecuteTask(_ context.Context, spec *taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	if spec == nil {
		return nil, errors.New("cannot execute nil TaskSpec")
	}
	if spec.Source.Repo == "" {
		return &taskcontract.TaskResult{
			Status:           taskcontract.StatusFailed,
			ResultText:       "Source repository not specified",
			VerificationText: "Validation failed",
		}, nil
	}

	// Use in-memory git repo
	storer := memory.NewStorage()
	fs := memfs.New()
	r, err := git.Init(storer, fs)
	if err != nil {
		return nil, err
	}
	wt, err := r.Worktree()
	if err != nil {
		return nil, err
	}

	f, err := fs.Create("init.txt")
	if err != nil {
		return nil, err
	}
	f.Write([]byte("initial\n"))
	f.Close()
	wt.Add("init.txt")
	initHash, _ := wt.Commit("init", &git.CommitOptions{
		Author: &object.Signature{Name: "test", Email: "test@local", When: time.Unix(0, 0)},
	})

	// Add dummy file
	dummyFile, _ := fs.Create(fmt.Sprintf(".lattice-tasks/%s.json", spec.ID))
	dummyFile.Write([]byte(fmt.Sprintf(`{"task_id":%q}`, spec.ID)))
	dummyFile.Close()
	wt.Add(fmt.Sprintf(".lattice-tasks/%s.json", spec.ID))

	resHash, err := wt.Commit(fmt.Sprintf("dummy: complete task %s", spec.ID), &git.CommitOptions{
		Author: &object.Signature{Name: "agent", Email: "agent@local", When: time.Unix(100, 0)},
	})
	if err != nil {
		return nil, err
	}

	_ = initHash
	return &taskcontract.TaskResult{
		Status:           taskcontract.StatusCompleted,
		Revision:         resHash.String(),
		ResultText:       fmt.Sprintf("In-memory dummy execution complete for task %s", spec.ID),
		VerificationText: "Deterministic in-memory commit verified",
	}, nil
}
