package git

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/godiff/internal/diff"
)

// Staged returns HEAD → index; Unstaged returns index → work tree,
// including untracked files. A partially staged file appears in both.
func (r *Repo) Staged(opts Options) ([]*diff.File, error) {
	return r.localChanges(opts, true)
}

func (r *Repo) Unstaged(opts Options) ([]*diff.File, error) {
	return r.localChanges(opts, false)
}

func (r *Repo) localChanges(opts Options, staged bool) ([]*diff.File, error) {
	args := diffArgs(opts)
	if staged {
		args = append(args, "--cached")
	}
	out, err := r.Git(append(args, "--")...)
	if err != nil {
		return nil, err
	}
	files := diff.Parse(out)
	if !staged {
		untracked, err := r.untrackedFiles()
		if err != nil {
			return nil, err
		}
		files = append(files, untracked...)
	}
	r.markWorkingTreeStatus(files)
	r.markGenerated(files, "")
	SortFiles(files)
	return files, nil
}

func pathInput(paths []string) (io.Reader, error) {
	if len(paths) == 0 {
		return nil, errors.New("no files selected")
	}
	var b bytes.Buffer
	for _, path := range paths {
		if !filepath.IsLocal(path) || strings.ContainsRune(path, 0) || strings.Split(filepath.ToSlash(path), "/")[0] == ".git" {
			return nil, errors.New("invalid repository path")
		}
		b.WriteString(path)
		b.WriteByte(0)
	}
	return &b, nil
}

func (r *Repo) changePaths(ctx context.Context, paths []string, args ...string) error {
	in, err := pathInput(paths)
	if err != nil {
		return err
	}
	args = append([]string{"--literal-pathspecs"}, args...)
	_, err = run(ctx, r.Root, in, append(args, "--pathspec-from-file=-", "--pathspec-file-nul")...)
	return err
}

func (r *Repo) Stage(ctx context.Context, paths []string) error {
	return r.changePaths(ctx, paths, "add", "-A")
}

// Unstage restores index entries without changing work tree files, including
// the initial commit where there is no HEAD to restore from.
func (r *Repo) Unstage(ctx context.Context, paths []string) error {
	if !r.HasHead() {
		return r.changePaths(ctx, paths, "rm", "--cached", "-r", "-f", "--ignore-unmatch")
	}
	return r.changePaths(ctx, paths, "restore", "--staged", "--source=HEAD")
}

// CanDiscard excludes directories and gitlinks: their contents belong to
// another repository or are intentionally not listed individually.
func CanDiscard(f *diff.File) bool {
	return !f.Directory && !f.Submodule && f.Status != diff.Conflict
}

// Discard restores tracked files from the index, preserving staged changes.
// Only explicitly listed untracked files are deleted, never directories.
func (r *Repo) Discard(ctx context.Context, files []*diff.File) error {
	var tracked, untracked []string
	for _, f := range files {
		if !CanDiscard(f) {
			return errors.New("cannot discard a directory, subrepository, or unresolved conflict")
		}
		if f.Status == diff.Untracked {
			untracked = append(untracked, f.Path)
		} else {
			tracked = append(tracked, f.Path)
		}
	}
	paths := append(append([]string{}, tracked...), untracked...)
	if _, err := pathInput(paths); err != nil {
		return err
	}
	if len(tracked) > 0 {
		if err := r.changePaths(ctx, tracked, "restore", "--worktree"); err != nil {
			return err
		}
	}
	for _, path := range untracked {
		if err := ctx.Err(); err != nil {
			return err
		}
		full := filepath.Join(r.Root, path)
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if info.IsDir() {
			return errors.New("cannot discard an untracked directory")
		}
		if err := os.Remove(full); err != nil {
			return err
		}
	}
	return nil
}
