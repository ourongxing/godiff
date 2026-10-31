package git

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSubrepositories(t *testing.T) {
	source, sourceRepo := newRepo(t)
	grand, _ := newRepo(t)
	gitIn(t, source, "-c", "protocol.file.allow=always", "submodule", "add", "-q", grand, "deps/grand")
	gitIn(t, source, "commit", "-qam", "Add nested submodule")
	dir, repo := newRepo(t)
	gitIn(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "modules/child space")
	gitIn(t, dir, "-c", "protocol.file.allow=always", "submodule", "update", "--init", "--recursive")
	gitIn(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "missing")
	gitIn(t, dir, "submodule", "deinit", "-f", "missing")
	write(t, dir, ".gitignore", "ignored/\n")
	if err := os.MkdirAll(filepath.Join(dir, "ignored"), 0o755); err != nil {
		t.Fatal(err)
	}
	gitIn(t, filepath.Join(dir, "ignored"), "init", "-q")
	// A nested worktree uses a .git file, like absorbed submodules do.
	gitIn(t, sourceRepo.Root, "worktree", "add", "-q", "-b", "nested-worktree", filepath.Join(dir, "extras", "work tree"))
	children, err := repo.Subrepositories()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, child := range children {
		got[child.Path] = child.Initialized
		if child.Initialized {
			opened, err := Open(child.Root)
			if err != nil || opened.Root != child.Root || opened.Root == repo.Root {
				t.Fatalf("wrong repository for %+v: %+v, %v", child, opened, err)
			}
		}
	}
	for path, initialized := range map[string]bool{
		"modules/child space": true, "modules/child space/deps/grand": true,
		"missing": false, "extras/work tree": true, "extras/work tree/deps/grand": false,
	} {
		if value, ok := got[path]; !ok || value != initialized {
			t.Errorf("%s: %v, present %v, want %v; all: %v", path, value, ok, initialized, got)
		}
	}
	if len(got) != 5 {
		t.Fatalf("unexpected repositories: %v", got)
	}
}

func TestSubmoduleDiffAndIndependentCommit(t *testing.T) {
	source, _ := newRepo(t)
	dir, parent := newRepo(t)
	gitIn(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "app")
	gitIn(t, dir, "commit", "-qam", "Add app")
	child, err := Open(filepath.Join(dir, "app"))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := parent.Resolve("HEAD")
	write(t, child.Root, "a.txt", "changed in child\n")
	files, err := parent.WorkingTree(Options{})
	if err != nil || len(files) != 1 || !files[0].Submodule || files[0].Path != "app" {
		patch, _ := parent.Git("diff", "--submodule=short", "HEAD")
		t.Fatalf("parent diff: %+v, %v\n%s", files, err, patch)
	}
	if _, err := child.CommitChanges("Only child", []string{"a.txt"}); err != nil {
		t.Fatal(err)
	}
	if after, _ := parent.Resolve("HEAD"); after != before {
		t.Fatal("committing the child changed the parent commit")
	}
	files, err = parent.WorkingTree(Options{})
	if err != nil || len(files) != 1 || !files[0].Submodule {
		t.Fatalf("the parent must still show its uncommitted pointer: %+v, %v", files, err)
	}
}
