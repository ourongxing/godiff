package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func repositoryTestWindow(t *testing.T) (*window, *window, *ui.Tester) {
	t.Helper()
	source := testRepo(t)
	gitIn(t, source, "add", "-A")
	gitIn(t, source, "commit", "-qm", "Child baseline")
	dir := testRepo(t)
	gitIn(t, dir, "-c", "protocol.file.allow=always", "submodule", "add", "-q", source, "app")
	gitIn(t, dir, "commit", "-qm", "Add submodule", "--", ".gitmodules", "app")
	writeFile(t, filepath.Join(dir, "app"), "main.go", strings.Replace(mainGo, "Hello, ", "Child says ", 1))
	root, _ := newTestWindow(t, dir)
	root.initRepositories()
	root.workspace.discover()
	if len(root.workspace.entries) != 2 || root.workspace.entries[1].view == nil {
		t.Fatalf("repositories: %+v", root.workspace.entries)
	}
	child := root.workspace.entries[1].view
	tt := ui.NewTester(root.view, 1280, 860)
	tt.SetDark(true)
	tt.Frame()
	return root, child, tt
}

func TestRepositorySwitchPreservesReviewsAndDrafts(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	root.subject, root.body = "Parent draft", "Parent summary"
	child.subject, child.body = "Child draft", "Child summary"
	root.workspace.discover()
	if root.workspace.entries[1].view != child || child.subject != "Child draft" {
		t.Fatal("rediscovery replaced an existing repository's state")
	}
	root.addComment("main.go", sideNew, 6, 6)
	root.comments[0].text = "Parent comment"
	child.addComment("main.go", sideNew, 6, 6)
	child.comments[0].text = "Child comment"
	tt.Frame()
	snapshot(t, tt, "repositories")
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if root.activeRepository() != child || child.subject != "Child draft" || root.subject != "Parent draft" {
		t.Fatal("switch mixed the repository state")
	}
	if err := tt.Click("Commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !child.commitOpen || root.commitOpen || child.body != "Child summary" || root.body != "Parent summary" {
		t.Fatal("commit draft belongs to the wrong repository")
	}
	root.activateRepository()
	tt.Frame()
	if root.comments[0].text != "Parent comment" || child.comments[0].text != "Child comment" {
		t.Fatal("reviews leaked between files with the same path")
	}
	// A held background load must still update its own state after switching.
	child.hold = true
	child.load()
	root.activateRepository()
	child.hold = false
	jobs := child.held
	child.held = nil
	for _, job := range jobs {
		job()
	}
	tt.Frame()
	if root.activeRepository() != root || !strings.Contains(strings.Join(child.files[0].newLines, "\n"), "Child says") {
		t.Fatal("late load applied to the active repository instead of its owner")
	}
	child.activateRepository()
	tt.Frame()
	snapshot(t, tt, "repository-commit")
}

func TestRepositoryCommitOnlyChangesChild(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	parentHead, _ := root.repo.Resolve("HEAD")
	child.subject = "Commit child only"
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Commit"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	tt.Frame()
	if child.commitDone == "" {
		t.Fatalf("child commit failed: %s %s", child.commitErr, child.commitOutput)
	}
	if after, _ := root.repo.Resolve("HEAD"); after != parentHead {
		t.Fatal("child commit also committed the parent")
	}
	if !strings.Contains(gitIn(t, root.repo.Root, "status", "--porcelain"), "main.go") {
		t.Fatal("the parent's file with the same path was committed")
	}
}

func TestRepositoryCollapseAndRemoval(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	if err := tt.Click("Toggle repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !root.workspace.entries[1].collapsed {
		t.Fatal("repository did not collapse")
	}
	if err := tt.Click("Toggle repository app"); err != nil {
		t.Fatal(err)
	}
	if root.workspace.entries[1].collapsed {
		t.Fatal("repository did not expand")
	}
	child.activateRepository()
	// Removing a child worktree leaves a visible, uninitialized gitlink.
	if err := os.Rename(child.repo.Root, child.repo.Root+"-moved"); err != nil {
		t.Fatal(err)
	}
	root.workspace.discover()
	tt.Frame()
	if root.activeRepository() != root || root.workspace.entries[1].view != nil || !tt.HasText("Submodule not initialized") {
		t.Fatal("removed submodule was mistaken for its parent repository")
	}
}
