package main

import (
	"slices"
	"testing"
)

func TestRemoteRepositoryLinkFollowsSelection(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	if _, ok := tt.Find("Open remote repository"); ok {
		t.Fatal("local repositories should not show a web link")
	}
	gitIn(t, root.repo.Root, "remote", "add", "origin", "git@github.com:acme/parent.git")
	gitIn(t, child.repo.Root, "remote", "set-url", "origin", "https://gitlab.com/team/child.git")
	gitIn(t, root.repo.Root, "switch", "-c", "feature/links")
	root.load()
	child.load()
	tt.Frame()
	snapshot(t, tt, "remote-repository")
	if err := tt.Click("Open remote repository"); err != nil {
		t.Fatal(err)
	}
	child.activateRepository()
	tt.Frame()
	if err := tt.Click("Open remote repository"); err != nil {
		t.Fatal(err)
	}
	want := []string{"https://github.com/acme/parent/tree/feature%2Flinks", "https://gitlab.com/team/child/-/tree/main"}
	if !slices.Equal(tt.OpenedURLs(), want) {
		t.Fatalf("opened %v, want %v", tt.OpenedURLs(), want)
	}
	gitIn(t, child.repo.Root, "remote", "remove", "origin")
	child.load()
	tt.Frame()
	if _, ok := tt.Find("Open remote repository"); ok {
		t.Fatal("removing the remote should remove the link after refresh")
	}
}
