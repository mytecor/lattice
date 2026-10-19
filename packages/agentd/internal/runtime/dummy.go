package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// ErrConflict indicates a conflict with existing content was detected.
var ErrConflict = errors.New("conflict: incompatible task payload already exists")

// DummyWorkloadData is the deterministic JSON structure written to .lattice-tasks/<id>.json.
type DummyWorkloadData struct {
	TaskID         string `json:"task_id"`
	TaskRepo       string `json:"task_repo"`
	TaskPath       string `json:"task_path"`
	TaskCommit     string `json:"task_commit"`
	SourceRepo     string `json:"source_repo"`
	SourceRevision string `json:"source_revision"`
}

// DummyRuntime prepares local Git repositories for dummy workload execution.
type DummyRuntime struct {
	workDir string
}

// NewDummyRuntime creates a new DummyRuntime.
func NewDummyRuntime(workDir string) *DummyRuntime {
	return &DummyRuntime{workDir: workDir}
}

// DummyInstance represents a local git workspace instance for a task.
type DummyInstance struct {
	endpoint string
	workDir  string
	commit   string
	cleaned  bool
}

func (i *DummyInstance) Endpoint() string   { return i.endpoint }
func (i *DummyInstance) WorkDir() string    { return i.workDir }
func (i *DummyInstance) Commit() string     { return i.commit }
func (i *DummyInstance) SetCommit(h string) { i.commit = h }
func (i *DummyInstance) Stop(_ context.Context) error {
	if i.cleaned {
		return nil
	}
	i.cleaned = true
	if i.workDir != "" {
		_ = os.RemoveAll(i.workDir)
	}
	return nil
}

// Start prepares the repository workspace, writes deterministic data, and creates a commit.
func (r *DummyRuntime) Start(ctx context.Context, spec *taskcontract.TaskSpec) (Instance, error) {
	if spec == nil {
		return nil, errors.New("cannot start with nil TaskSpec")
	}
	if spec.Source.Repo == "" {
		return nil, errors.New("source repository not specified in TaskSpec")
	}

	sourceRepoPath := strings.TrimPrefix(spec.Source.Repo, "file://")

	repo, err := git.PlainOpen(sourceRepoPath)
	if err != nil {
		return nil, fmt.Errorf("open source repository at %s: %w", sourceRepoPath, err)
	}

	var targetHash plumbing.Hash
	if spec.Source.Revision != "" {
		h, err := repo.ResolveRevision(plumbing.Revision(spec.Source.Revision))
		if err != nil {
			return nil, fmt.Errorf("resolve source revision %s: %w", spec.Source.Revision, err)
		}
		targetHash = *h
	} else {
		headRef, err := repo.Head()
		if err != nil {
			return nil, fmt.Errorf("resolve source HEAD: %w", err)
		}
		targetHash = headRef.Hash()
	}

	relFilePath := filepath.Join(".lattice-tasks", fmt.Sprintf("%s.json", spec.ID))

	// Check commit tree for existing commit (idempotency or conflict)
	checkCommitTree := func(commitHash plumbing.Hash) (*DummyInstance, error) {
		cObj, err := repo.CommitObject(commitHash)
		if err != nil {
			return nil, nil
		}
		cTree, err := cObj.Tree()
		if err != nil {
			return nil, nil
		}
		f, err := cTree.File(relFilePath)
		if err != nil {
			return nil, nil
		}
		rdr, err := f.Reader()
		if err != nil {
			return nil, nil
		}
		defer rdr.Close()
		existingBytes, err := io.ReadAll(rdr)
		if err != nil {
			return nil, nil
		}

		var existingData DummyWorkloadData
		if err := json.Unmarshal(existingBytes, &existingData); err == nil {
			if existingData.TaskID == spec.ID &&
				existingData.TaskCommit == spec.Commit &&
				existingData.TaskRepo == spec.TaskRepo &&
				existingData.TaskPath == spec.Path {
				return &DummyInstance{
					endpoint: fmt.Sprintf("dummy://%s", spec.ID),
					commit:   commitHash.String(),
				}, nil
			}
		}

		return nil, fmt.Errorf("%w: %s has differing task payload in commit %s", ErrConflict, relFilePath, commitHash.String())
	}

	if inst, err := checkCommitTree(targetHash); err != nil {
		return nil, err
	} else if inst != nil {
		return inst, nil
	}

	if headRef, hErr := repo.Head(); hErr == nil && headRef.Hash() != targetHash {
		if inst, err := checkCommitTree(headRef.Hash()); err != nil {
			return nil, err
		} else if inst != nil {
			return inst, nil
		}
	}

	// Prepare clone workspace
	tempDir, err := os.MkdirTemp(r.workDir, fmt.Sprintf("agentd-work-%s-", spec.ID))
	if err != nil {
		return nil, fmt.Errorf("create temp workdir: %w", err)
	}

	workRepo, err := git.PlainCloneContext(ctx, tempDir, false, &git.CloneOptions{
		URL:           sourceRepoPath,
		ReferenceName: plumbing.ReferenceName("refs/heads/main"),
		SingleBranch:  false,
	})
	if err != nil {
		workRepo, err = git.PlainCloneContext(ctx, tempDir, false, &git.CloneOptions{
			URL: sourceRepoPath,
		})
		if err != nil {
			_ = os.RemoveAll(tempDir)
			return nil, fmt.Errorf("clone source repository: %w", err)
		}
	}

	worktree, err := workRepo.Worktree()
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("get worktree: %w", err)
	}

	if err := worktree.Checkout(&git.CheckoutOptions{
		Hash: targetHash,
	}); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("checkout target revision %s: %w", targetHash.String(), err)
	}

	// Write deterministic file
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
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("marshal dummy workload data: %w", err)
	}
	expectedJSON = append(expectedJSON, '\n')

	destFilePath := filepath.Join(tempDir, relFilePath)
	if err := os.MkdirAll(filepath.Dir(destFilePath), 0755); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("create directory for %s: %w", relFilePath, err)
	}
	if err := os.WriteFile(destFilePath, expectedJSON, 0644); err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("write %s: %w", relFilePath, err)
	}

	if _, err := worktree.Add(relFilePath); err != nil {
		_ = os.RemoveAll(tempDir)
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
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("git commit: %w", err)
	}

	// Push/update origin
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

	if targetHead, rErr := repo.Head(); rErr == nil && targetHead.Name().IsBranch() {
		_ = repo.Storer.SetReference(plumbing.NewHashReference(targetHead.Name(), commitHash))
	}

	return &DummyInstance{
		endpoint: fmt.Sprintf("dummy://%s", spec.ID),
		workDir:  tempDir,
		commit:   commitHash.String(),
	}, nil
}
