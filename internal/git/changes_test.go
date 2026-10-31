package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/godiff/internal/diff"
)

func TestStageUnstageAndDiscardPartialChanges(t *testing.T) {
	dir, r := newRepo(t)
	ctx := context.Background()
	write(t, dir, "a.txt", "one\nstaged\nthree\n")
	if err := r.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "one\nunstaged\nthree\n")
	staged, err := r.Staged(Options{})
	if err != nil || len(staged) != 1 || staged[0].Path != "a.txt" {
		t.Fatalf("staged: %+v, %v", staged, err)
	}
	unstaged, err := r.Unstaged(Options{})
	if err != nil || len(unstaged) != 1 || unstaged[0].Path != "a.txt" {
		t.Fatalf("unstaged: %+v, %v", unstaged, err)
	}
	if staged[0].Hunks[0].Lines[1].Text != "two" || unstaged[0].Hunks[0].Lines[1].Text != "staged" {
		t.Fatal("diffs do not use HEAD → index and index → work tree")
	}
	write(t, dir, "new file.txt", "untracked\n")
	unstaged, _ = r.Unstaged(Options{})
	if err := r.Discard(ctx, unstaged); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "one\nstaged\nthree\n" {
		t.Fatalf("discard overwrote staged content: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "new file.txt")); !os.IsNotExist(err) {
		t.Fatal("untracked file was not discarded")
	}
	if got := gitIn(t, dir, "show", ":a.txt"); got != "one\nstaged\nthree\n" {
		t.Fatalf("index changed: %q", got)
	}
	if err := r.Unstage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, dir, "show", ":a.txt"); got != "one\ntwo\nthree\n" {
		t.Fatalf("unstage did not restore index: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "one\nstaged\nthree\n" {
		t.Fatal("unstage changed the work tree")
	}
}

func TestIndexOperationsHandleRenamesDeletesAndLiteralPaths(t *testing.T) {
	dir, r := newRepo(t)
	ctx := context.Background()
	paths := []string{"space name.txt", "line\nbreak.txt", ":(glob)*", "-option.txt", "empty.txt", "binary.dat"}
	for _, p := range paths {
		write(t, dir, p, "new\n")
	}
	write(t, dir, "empty.txt", "")
	write(t, dir, "binary.dat", "\x00binary")
	if err := r.Stage(ctx, paths); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "mv", "src/b.go", "src/renamed.go")
	if err := os.Remove(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if err := r.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	files, err := r.Staged(Options{})
	if err != nil || len(files) != len(paths)+2 {
		t.Fatalf("staged: %d files, %v", len(files), err)
	}
	var all []string
	for _, f := range files {
		all = append(all, f.Path)
		if f.Status == diff.Renamed {
			all = append(all, f.OldPath)
		}
	}
	if err := r.Unstage(ctx, all); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, dir, "diff", "--cached", "--stat"); got != "" {
		t.Fatalf("index still changed: %s", got)
	}
	for _, p := range append(paths, "src/renamed.go") {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Fatalf("unstage removed %s: %v", p, err)
		}
	}
}

func TestIndexOperationsBeforeFirstCommit(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q")
	r := &Repo{Root: dir}
	ctx := context.Background()
	write(t, dir, "a.txt", "staged\n")
	if err := r.Stage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	write(t, dir, "a.txt", "later\n")
	if files, err := r.Staged(Options{}); err != nil || len(files) != 1 || files[0].Status != diff.Added {
		t.Fatalf("initial staged: %+v %v", files, err)
	}
	if err := r.Unstage(ctx, []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, dir, "ls-files"); got != "" {
		t.Fatal("initial index not empty")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "later\n" {
		t.Fatal("initial unstage changed work tree")
	}
}

func TestDiscardRejectsDirectoriesSubrepositoriesAndInvalidPaths(t *testing.T) {
	dir, r := newRepo(t)
	ctx := context.Background()
	for _, f := range []*diff.File{
		{Path: "src", Directory: true, Status: diff.Untracked},
		{Path: "src", Submodule: true},
		{Path: "a.txt", Status: diff.Conflict},
		{Path: "../outside", Status: diff.Untracked},
		{Path: ".git/config", Status: diff.Untracked},
	} {
		if err := r.Discard(ctx, []*diff.File{f}); err == nil {
			t.Fatalf("discard accepted %+v", f)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "src/b.go")); err != nil {
		t.Fatal("discard touched directory contents")
	}
	if err := r.Stage(ctx, []string{"../outside"}); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatal("stage accepted outside path")
	}
}
