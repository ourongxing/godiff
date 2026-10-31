package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/agent"
	"github.com/egoist/godiff/internal/github"
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
	t.Cleanup(func() {
		for _, entry := range root.workspace.entries {
			if entry.view != nil {
				entry.view.dispose()
			}
		}
	})
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
	hoverSidebarRow(t, tt, "Repository app")
	if err := tt.Click("Review local changes in app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if root.activeRepository() != child || child.subject != "Child draft" || root.subject != "Parent draft" {
		t.Fatal("switch mixed the repository state")
	}
	child.toggleCommit()
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
	hoverSidebarRow(t, tt, "Repository app")
	if err := tt.Click("Review local changes in app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	child.paletteOpen = true
	tt.Frame()
	tt.Type("commit")
	tt.Frame()
	if err := tt.Click("Commit…"); err != nil {
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
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !root.workspace.entries[1].collapsed {
		t.Fatal("repository did not collapse")
	}
	if root.activeRepository() != root {
		t.Fatal("collapsing a repository selected it")
	}
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	if root.workspace.entries[1].collapsed {
		t.Fatal("repository did not expand")
	}
	if root.activeRepository() != root {
		t.Fatal("expanding a repository selected it")
	}
	tt.Move(500, 50)
	tt.Frame()
	snapshot(t, tt, "repository-group-expanded")
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
	if child.ctx.Err() != context.Canceled {
		t.Fatal("removed repository still owns live background jobs")
	}
	child.update(func() { root.subject = "late update" })
	if root.subject != "" {
		t.Fatal("a removed repository published a late result")
	}
	if err := os.Rename(child.repo.Root+"-moved", child.repo.Root); err != nil {
		t.Fatal(err)
	}
	root.workspace.discover()
	reopened := root.workspace.entries[1].view
	if reopened == nil || reopened == child || reopened.ctx.Err() != nil || len(reopened.files) == 0 {
		t.Fatal("reinitialized submodule did not acquire fresh, usable state")
	}
}

func TestRepositoryHeaderActionsStayWithTheirRepository(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	gitIn(t, child.repo.Root, "remote", "set-url", "origin", "https://gitlab.com/team/child.git")
	gitIn(t, child.repo.Root, "switch", "-c", "feature/header-actions")
	head, err := child.repo.Resolve("HEAD")
	if err != nil {
		t.Fatal(err)
	}
	child.setSource(source{kind: sourceCommit, ref: head})
	root.tab = 1
	tt.Frame()
	hoverSidebarRow(t, tt, "Repository app")
	snapshot(t, tt, "repository-header-history-actions")
	if err := tt.Click("Open remote repository for app"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tt.OpenedURLs(), []string{"https://gitlab.com/team/child/-/tree/feature%2Fheader-actions"}) || root.activeRepository() != root {
		t.Fatal("the header opened another repository or changed the selection")
	}
	gitIn(t, child.repo.Root, "commit", "-qam", "Update child")
	parentGen, childGen := root.gen, child.gen
	if err := tt.Click("Refresh repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if child.gen != childGen+1 || root.gen != parentGen || root.activeRepository() != root || child.history[0].Subject != "Update child" || child.source.ref != head {
		t.Fatal("the header refresh changed another repository or the selected review")
	}
	if err := tt.Click("Review local changes in app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if root.activeRepository() != child || child.tab != 0 || child.source != (source{}) || root.source != (source{}) {
		t.Fatal("the header did not open the child's local changes")
	}
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	hoverSidebarRow(t, tt, "Repository app")
	if _, ok := tt.Find("Refresh repository app"); !ok || !root.workspace.entries[1].collapsed {
		t.Fatal("collapsing a repository hid its actions")
	}
	snapshot(t, tt, "repository-header-collapsed-actions")
}

func TestRepositoryHistorySwitch(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	root.historyFilter = "Add submodule"
	if err := tt.Click("History (⌘2)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Add submodule") || tt.HasText("Child baseline") {
		t.Fatal("History should initially show the parent's commits")
	}
	snapshot(t, tt, "repository-history-parent")
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if root.activeRepository() != root || !tt.HasText("Child baseline") || tt.HasText("Add submodule") {
		t.Fatal("expanding the child's history selected a review or showed the wrong commits")
	}
	// Expanding the group hands keyboard navigation to its history list.
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if child.source.kind != sourceCommit || child.source.ref != child.history[0].Hash ||
		child.commit == nil || child.commit.Subject != "Child baseline" || root.source.kind != sourceWorkingTree {
		t.Fatal("history navigation selected a commit in the wrong repository")
	}
	snapshot(t, tt, "repository-history-child")
	if err := tt.Click("Filter history"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Child baseline")
	tt.Frame()
	if child.historyFilter != "Child baseline" {
		t.Fatal("the child history filter was not updated")
	}
	if err := tt.Click("Repository " + filepath.Base(root.repo.Root)); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if root.historyFilter != "Add submodule" || child.historyFilter != "Child baseline" || !tt.HasText("Add submodule") {
		t.Fatal("switching repositories mixed their history filters")
	}
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if child.source.ref != child.history[0].Hash || !tt.HasText("Child baseline") {
		t.Fatal("switching back lost the child's selected commit")
	}
}

func TestRepositoryHistoryCollapseAndPagination(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	// Inactive histories must not paginate merely because their headers show.
	child.historyMore = true
	limit := child.historyLimit
	if err := tt.Click("History (⌘2)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if child.historyLimit != limit {
		t.Fatal("the inactive repository loaded additional history")
	}
	name := filepath.Base(root.repo.Root)
	if err := tt.Click("Repository " + name); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if tt.HasText("Add submodule") || !root.workspace.entries[0].historyCollapsed {
		t.Fatal("the repository history did not collapse")
	}
	if err := tt.Click("Files (⌘1)"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if root.workspace.entries[0].collapsed || !tt.HasText("long.txt") {
		t.Fatal("collapsing history should not collapse the file tree")
	}
}

func TestRepositoryPullRequests(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	head, _ := child.repo.Resolve("HEAD")
	server := fakeGitHub{base: head, head: head, comments: `[]`}
	server.serve(t)
	child.gh = ghRemote{repo: github.Repo{Owner: "acme", Name: "app"}, remote: "origin"}
	root.showPulls()
	tt.Frame()
	if root.tab != 2 || !tt.HasText("This repository has no GitHub remote.") || child.pullsLoaded {
		t.Fatal("the parent's missing remote hid the child's pull requests or fetched them before selection")
	}
	if err := tt.Click("Repository app"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if root.activeRepository() != root || !child.pullsLoaded || !tt.HasText("Greet louder") {
		t.Fatal("expanding the child's pull requests selected a review or used another remote")
	}
	snapshot(t, tt, "repository-pulls")
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if child.source != pullSource(child.gh.repo, 1) || child.pr == nil || child.pr.meta == nil || child.loadErr != nil || root.source.kind != sourceWorkingTree {
		t.Fatalf("pull request selection used the wrong repository: source=%+v, error=%v", child.source, child.loadErr)
	}
	if err := tt.Click("Filter pull requests"); err != nil {
		t.Fatal(err)
	}
	tt.Type("greet")
	tt.Frame()
	root.activateRepository()
	tt.Frame()
	if root.tab != 2 || root.pullsFilter != "" || child.pullsFilter != "greet" {
		t.Fatal("repository switching lost the Pull Requests tab or mixed its filters")
	}
	child.activateRepository()
	tt.Frame()
	if child.source != pullSource(child.gh.repo, 1) || !tt.HasText("Greet louder") {
		t.Fatal("switching repositories lost the selected pull request")
	}
}

func TestRepositoryPaletteWithoutSidebar(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	root.sidebarShown = false
	root.paletteOpen = true
	tt.Frame()
	tt.Type("Switch Repository: app")
	tt.Frame()
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if root.activeRepository() != child || child.sidebarShown || root.paletteOpen {
		t.Fatal("the command bar could not switch repositories with the sidebar hidden")
	}
	// Palette actions after switching also belong to the active repository.
	gitIn(t, child.repo.Root, "branch", "child-base")
	child.openDialog(dialogBranch)
	tt.Frame()
	tt.Type("child-base")
	tt.Frame()
	if err := tt.Click("Open"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if child.source != (source{kind: sourceBranch, ref: "child-base"}) || child.loadErr != nil || root.source.kind != sourceWorkingTree {
		t.Fatalf("branch comparison did not use the child: %+v, %v", child.source, child.loadErr)
	}
}

func TestRepositoryRefreshAndPolling(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	selected := child.history[len(child.history)-1].Hash
	child.setSource(source{kind: sourceCommit, ref: selected})
	child.activateRepository()
	gitIn(t, child.repo.Root, "switch", "-c", "feature/child")
	writeFile(t, child.repo.Root, "poll.txt", "external child commit\n")
	gitIn(t, child.repo.Root, "add", "poll.txt")
	gitIn(t, child.repo.Root, "commit", "-qm", "External child commit", "--", "poll.txt")
	writeFile(t, root.repo.Root, "poll.txt", "external parent commit\n")
	gitIn(t, root.repo.Root, "add", "poll.txt")
	gitIn(t, root.repo.Root, "commit", "-qm", "External parent commit", "--", "poll.txt")
	root.checkRepositoryChanges()
	tt.Frame()
	if child.history[0].Subject != "External child commit" || root.history[0].Subject != "External parent commit" || child.branch != "feature/child" {
		t.Fatal("polling missed a parent's or child's new commits or checked-out branch")
	}
	if child.source.ref != selected || child.commit.Hash != selected {
		t.Fatal("history refresh changed the commit being reviewed")
	}
	writeFile(t, root.repo.Root, "refresh-parent.txt", "parent\n")
	writeFile(t, child.repo.Root, "refresh-child.txt", "child\n")
	child.setSource(source{})
	child.refresh()
	var parentFound, childFound bool
	for _, f := range root.files {
		parentFound = parentFound || f.Path == "refresh-parent.txt"
	}
	for _, f := range child.files {
		childFound = childFound || f.Path == "refresh-child.txt"
	}
	if !parentFound || !childFound || root.activeRepository() != child {
		t.Fatal("Refresh did not update all repositories while preserving selection")
	}
}

func TestRepositoryHistoryDropsOlderLoad(t *testing.T) {
	_, child, _ := repositoryTestWindow(t)
	child.hold = true
	child.loadHistory()
	child.historyLimit = 1
	child.loadHistory()
	jobs := child.held
	child.held, child.hold = nil, false
	jobs[1]()
	jobs[0]()
	if len(child.history) != 1 || child.historyLoading {
		t.Fatal("an older history response overwrote a newer request")
	}
}

func TestRepositoryFindAndAI(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	child.activateRepository()
	child.finding, child.query = true, "Child says"
	tt.Frame()
	if len(child.matches) == 0 || root.query != "" || len(root.matches) != 0 {
		t.Fatal("diff search used or modified the parent repository")
	}
	old := runAgent
	t.Cleanup(func() { runAgent = old })
	child.settings.AIAgent = "claude"
	var called bool
	runAgent = func(ctx context.Context, a agent.Agent, req agent.Request, progress func(string)) (*agent.Analysis, error) {
		called = true
		if req.Dir != child.repo.Root || !req.Checkout || len(req.Files) != len(child.files) {
			t.Fatalf("AI request used the wrong repository: %+v", req)
		}
		progress("Child review")
		child.dispose()
		if ctx.Err() != context.Canceled {
			t.Fatal("removing the repository did not cancel its AI review")
		}
		progress("Late result")
		return nil, ctx.Err()
	}
	child.runAnalysis()
	if !called || child.analyses[child.source].progress != "Child review" || root.analyses[root.source].progress != "" || root.analyses[root.source].running {
		t.Fatal("AI state leaked into the parent or accepted results after disposal")
	}
}

func TestRepositoryModelConfiguration(t *testing.T) {
	root, child, _ := repositoryTestWindow(t)
	modelsFound.Lock()
	previous := modelsFound.byScope
	modelsFound.byScope = map[modelScope]*agentModels{
		{"claude", root.repo.Root}: {done: true, def: "parent-model", fetchedAt: time.Now()},
	}
	modelsFound.Unlock()
	t.Cleanup(func() {
		modelsFound.Lock()
		modelsFound.byScope = previous
		modelsFound.Unlock()
	})
	a, _ := agent.ByName("claude")
	if modelsOf(a, root.repo.Root).def != "parent-model" || modelsOf(a, child.repo.Root).def != "" {
		t.Fatal("the parent's agent model configuration leaked into its child")
	}
}
