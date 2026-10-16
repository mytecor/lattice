package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

var (
	ErrRemoteNotSupported = errors.New("remote repositories not supported; git-watchd only watches local repositories")
	ErrRepoNotFound       = errors.New("repository directory not found")
	ErrNotGitRepo         = errors.New("path is not a valid git repository")
	ErrRefNotFound        = errors.New("ref not found")
	ErrPathOutsideAllowed = errors.New("repository path outside allowed directory")
)

// GitEngine defines operations on local git repositories.
type GitEngine interface {
	ValidateLocalRepo(ctx context.Context, repo string) error
	ResolveHead(ctx context.Context, repo, ref string) (string, error)
	DiffPaths(ctx context.Context, repo, from, to string) ([]string, error)
}

// GoGitEngine executes git operations in-process using go-git.
type GoGitEngine struct {
	AllowedPrefix string
}

// NewGoGitEngine creates a GoGitEngine.
func NewGoGitEngine(allowedPrefix string) *GoGitEngine {
	if allowedPrefix != "" {
		allowedPrefix = filepath.Clean(allowedPrefix)
	}
	return &GoGitEngine{
		AllowedPrefix: allowedPrefix,
	}
}

func (e *GoGitEngine) ValidateLocalRepo(ctx context.Context, repo string) error {
	if repo == "" {
		return errors.New("empty repository path")
	}

	// Reject remote URLs
	if strings.Contains(repo, "://") || strings.HasPrefix(repo, "git@") || (strings.Contains(repo, ":") && !strings.HasPrefix(repo, "/") && !strings.HasPrefix(repo, ".")) {
		return fmt.Errorf("%w: %q", ErrRemoteNotSupported, repo)
	}

	cleanPath := filepath.Clean(repo)
	if e.AllowedPrefix != "" {
		rel, err := filepath.Rel(e.AllowedPrefix, cleanPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("%w: %s is not within %s", ErrPathOutsideAllowed, cleanPath, e.AllowedPrefix)
		}
	}

	fi, err := os.Stat(cleanPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %s", ErrRepoNotFound, cleanPath)
		}
		return err
	}
	if !fi.IsDir() {
		return fmt.Errorf("%w: %s is not a directory", ErrRepoNotFound, cleanPath)
	}

	_, err = git.PlainOpenWithOptions(cleanPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return fmt.Errorf("%w: %s (%v)", ErrNotGitRepo, cleanPath, err)
	}

	return nil
}

func (e *GoGitEngine) ResolveHead(ctx context.Context, repo, ref string) (string, error) {
	if err := e.ValidateLocalRepo(ctx, repo); err != nil {
		return "", err
	}

	cleanPath := filepath.Clean(repo)
	r, err := git.PlainOpenWithOptions(cleanPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return "", fmt.Errorf("%w: %s (%v)", ErrNotGitRepo, cleanPath, err)
	}

	if ref == "" || ref == "HEAD" {
		headRef, err := r.Head()
		if err != nil {
			return "", fmt.Errorf("%w: HEAD in %s (%v)", ErrRefNotFound, cleanPath, err)
		}
		return headRef.Hash().String(), nil
	}

	// 1. Try ResolveRevision (handles full/short hashes, branches, tags, HEAD~N, etc.)
	if hash, err := r.ResolveRevision(plumbing.Revision(ref)); err == nil {
		// Verify object exists as commit or peel if tag
		if commit, err := r.CommitObject(*hash); err == nil {
			return commit.Hash.String(), nil
		}
		if tag, err := r.TagObject(*hash); err == nil {
			return tag.Target.String(), nil
		}
	}

	// 2. Try reference lookup (e.g. refs/heads/main, refs/tags/v1, or plain branch name)
	candidates := []plumbing.ReferenceName{
		plumbing.ReferenceName(ref),
		plumbing.NewBranchReferenceName(ref),
		plumbing.NewTagReferenceName(ref),
		plumbing.NewRemoteReferenceName("origin", ref),
	}
	for _, candidate := range candidates {
		refObj, err := r.Reference(candidate, true)
		if err == nil {
			hash := refObj.Hash()
			if tag, err := r.TagObject(hash); err == nil {
				hash = tag.Target
			}
			if _, err := r.CommitObject(hash); err == nil {
				return hash.String(), nil
			}
		}
	}

	return "", fmt.Errorf("%w: ref %q in %s", ErrRefNotFound, ref, cleanPath)
}

func (e *GoGitEngine) DiffPaths(ctx context.Context, repo, from, to string) ([]string, error) {
	if err := e.ValidateLocalRepo(ctx, repo); err != nil {
		return nil, err
	}
	cleanPath := filepath.Clean(repo)

	if to == "" {
		return nil, errors.New("target revision 'to' cannot be empty")
	}

	r, err := git.PlainOpenWithOptions(cleanPath, &git.PlainOpenOptions{DetectDotGit: true})
	if err != nil {
		return nil, fmt.Errorf("%w: %s (%v)", ErrNotGitRepo, cleanPath, err)
	}

	toCommit, err := r.CommitObject(plumbing.NewHash(to))
	if err != nil {
		return nil, fmt.Errorf("target revision %q not found in repository: %w", to, err)
	}

	toTree, err := toCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("read target tree for %q: %w", to, err)
	}

	if from == "" {
		// Root commit diff: list all files in the tree
		var paths []string
		err = toTree.Files().ForEach(func(f *object.File) error {
			paths = append(paths, f.Name)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("iterate target tree files: %w", err)
		}
		return paths, nil
	}

	fromCommit, err := r.CommitObject(plumbing.NewHash(from))
	if err != nil {
		return nil, fmt.Errorf("after_revision %q does not exist in repository: %w", from, err)
	}

	fromTree, err := fromCommit.Tree()
	if err != nil {
		return nil, fmt.Errorf("read from tree for %q: %w", from, err)
	}

	changes, err := object.DiffTree(fromTree, toTree)
	if err != nil {
		return nil, fmt.Errorf("diff trees: %w", err)
	}

	var paths []string
	seen := make(map[string]bool)
	for _, c := range changes {
		var name string
		if c.To.Name != "" {
			name = c.To.Name
		} else {
			name = c.From.Name
		}
		if name != "" && !seen[name] {
			seen[name] = true
			paths = append(paths, name)
		}
	}
	if paths == nil {
		paths = []string{}
	}
	return paths, nil
}
