package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func hoverSidebarRow(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	r, ok := tt.Find(label)
	if !ok {
		t.Fatalf("missing row %q", label)
	}
	tt.Move(r.X+r.W/2, r.Y+r.H/2)
	tt.Frame()
}

func TestChangesReviewSeparateRevisionsAndFileActions(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "add", "main.go")
	index := strings.Replace(mainGo, `"Hello, " + name`, `"Hi, " + name + "!"`, 1)
	writeFile(t, dir, "main.go", strings.Replace(index, "Hi, ", "Unstaged, ", 1))
	w, tt := newTestWindow(t, dir)
	tt.SetDark(true)
	if w.treeCount("s:staged") != 1 || w.treeCount("s:unstaged") != 4 {
		t.Fatalf("wrong groups: %d/%d", w.treeCount("s:staged"), w.treeCount("s:unstaged"))
	}
	if err := tt.Click("Staged file main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.source.ref != "staged" || len(w.files) != 1 || strings.Join(w.files[0].oldLines, "\n") != strings.TrimSuffix(mainGo, "\n") || strings.Join(w.files[0].newLines, "\n") != strings.TrimSuffix(index, "\n") {
		t.Fatalf("staged diff used wrong revisions: %+v, %+v", w.source, w.files[0])
	}
	w.addComment("main.go", sideNew, 6, 6)
	w.comments[0].text = "Index comment"
	if err := tt.Click("Changed file main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	var main *fileState
	for _, f := range w.files {
		if f.Path == "main.go" {
			main = f
		}
	}
	if w.source.ref != "unstaged" || main == nil || !strings.Contains(strings.Join(main.oldLines, "\n"), "Hi, ") || !strings.Contains(strings.Join(main.newLines, "\n"), "Unstaged, ") || len(w.comments) != 0 {
		t.Fatal("unstaged diff or review scope is incorrect")
	}
	snapshot(t, tt, "changes-partial")
	hoverSidebarRow(t, tt, "Changed file main.go")
	if err := tt.Click("Stage main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.changeErr != "" || w.treeCount("s:staged") != 1 || w.treeCount("s:unstaged") != 3 {
		t.Fatalf("file stage failed: %s", w.changeErr)
	}
	if err := tt.Click("Staged file main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(w.comments) != 1 || w.comments[0].text != "Index comment" {
		t.Fatal("switching groups lost the review")
	}
	hoverSidebarRow(t, tt, "Staged file main.go")
	if err := tt.Click("Unstage main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.changeErr != "" || len(w.staged) != 0 || len(w.unstaged) != 4 {
		t.Fatalf("file unstage failed: %s", w.changeErr)
	}
}

func TestChangeGroupsKeepFiltersAndKindGrouping(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "add", "main.go")
	w, tt := newTestWindow(t, dir)
	w.setGrouping(groupKind)
	tt.Frame()
	for i, f := range w.files {
		if f.Path == "main.go" {
			w.selectTreeFile(i)
			if w.treeItems[w.treeSel] == nil || w.treeItems[w.treeSel].scope != "staged" {
				t.Fatal("combined review did not select the staged-only file")
			}
		}
	}
	w.reviewChanges("staged", "main.go")
	tt.Frame()
	if w.treeItems["g:staged:code"] == nil || w.treeItems["g:docs"] == nil || w.treeCount("g:docs") != 1 {
		t.Fatal("switching change scopes lost another group's file kinds")
	}
	w.filter = "long"
	w.buildTree()
	tt.Frame()
	if w.treeCount("s:staged") != 0 || w.treeCount("s:unstaged") != 1 {
		t.Fatal("file filter did not apply to both change groups")
	}
	tt.SetDark(false)
	snapshot(t, tt, "changes-filter-light")
}

func TestChangeGroupsKeepOffsettingChangesVisible(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "add", "main.go")
	writeFile(t, dir, "main.go", mainGo)
	gitIn(t, dir, "restore", "docs/long.txt", "old.txt")
	if err := os.Remove(filepath.Join(dir, "src/new.go")); err != nil {
		t.Fatal(err)
	}
	w, tt := newTestWindow(t, dir)
	if w.tab != 0 || len(w.files) != 0 || len(w.staged) != 1 || len(w.unstaged) != 1 || !tt.HasText("No changes against HEAD") {
		t.Fatal("offsetting staged and unstaged changes were hidden")
	}
	w.reviewChanges("staged", "main.go")
	tt.Frame()
	if len(w.files) != 1 {
		t.Fatal("offsetting staged diff cannot be opened")
	}
}

func TestChangeGroupsCollapseAndBulkActions(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	tt.SetDark(true)
	if err := tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !w.closedDirs["s:unstaged"] || len(w.visibleTree()) != 2 {
		t.Fatal("change group did not collapse")
	}
	if err := tt.Click("Stage all changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.changeErr != "" || len(w.staged) != 4 || len(w.unstaged) != 0 {
		t.Fatalf("bulk stage failed: %s", w.changeErr)
	}
	if err := tt.Click("Review staged changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.source.ref != "staged" || len(w.files) != 4 {
		t.Fatal("group review did not open its diff")
	}
	snapshot(t, tt, "changes-staged")
	if err := tt.Click("Unstage all changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.changeErr != "" || len(w.staged) != 0 || len(w.unstaged) != 4 {
		t.Fatalf("bulk unstage failed: %s", w.changeErr)
	}
	if err := tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "changes-unstaged")
}

func TestDiscardChangesConfirmationKeepsIndex(t *testing.T) {
	dir := testRepo(t)
	gitIn(t, dir, "add", "main.go")
	index := gitIn(t, dir, "show", ":main.go")
	writeFile(t, dir, "main.go", strings.Replace(index, "Hi, ", "Unstaged, ", 1))
	w, tt := newTestWindow(t, dir)
	if err := tt.Click("Discard all changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(w.discardChanges) != 4 {
		t.Fatal("no discard confirmation")
	}
	if err := tt.Click("Cancel"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if len(w.discardChanges) != 0 || len(w.unstaged) != 4 {
		t.Fatal("cancel discarded changes")
	}
	if err := tt.Click("Discard all changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	snapshot(t, tt, "changes-discard")
	if err := tt.Click("Discard Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if w.changeErr != "" || len(w.unstaged) != 0 || len(w.staged) != 1 {
		t.Fatalf("discard result: %s, %d/%d", w.changeErr, len(w.staged), len(w.unstaged))
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "main.go")); string(got) != index {
		t.Fatal("discard did not preserve staged content")
	}
	if _, err := os.Stat(filepath.Join(dir, "src/new.go")); !os.IsNotExist(err) {
		t.Fatal("untracked file remained")
	}
}

func TestSubrepositoryStagingIsIndependent(t *testing.T) {
	root, child, tt := repositoryTestWindow(t)
	before := gitIn(t, root.repo.Root, "diff", "--cached")
	child.changeIndex(false, child.unstaged)
	tt.Frame()
	if child.changeErr != "" || root.activeRepository() != child || len(child.staged) != 1 || len(child.unstaged) != 0 {
		t.Fatalf("child stage failed: %s", child.changeErr)
	}
	if got := gitIn(t, root.repo.Root, "diff", "--cached"); got != before {
		t.Fatal("child stage modified parent's index")
	}
	snapshot(t, tt, "changes-subrepositories")
	child.changeIndex(true, child.staged)
	tt.Frame()
	if len(child.staged) != 0 || len(child.unstaged) != 1 || len(root.staged) != 0 {
		t.Fatal("child unstage changed wrong repository")
	}
}
