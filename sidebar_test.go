package main

import (
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFileTreeSelectsOnPressFromOutsideSidebar(t *testing.T) {
	w, _ := newTestWindow(t, testRepo(t))
	w.treeSel = "f:main.go"
	draft := ""
	var accent ui.Color
	tt := ui.NewTester(func(c *ui.Context) {
		accent = c.Theme().Accent
		ui.Column(c).Fill().Children(func() {
			ui.Text(c, "Selected "+w.treeSel)
			w.fileTree(c)
			ui.TextInput(c, &draft).Label("Editor")
		})
	}, 300, 500)
	if err := tt.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	old, ok := tt.Find("main.go")
	if !ok {
		t.Fatal("the selected file row is missing")
	}
	r, ok := tt.Find("old.txt")
	if !ok {
		t.Fatal("the file row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "f:old.txt" || !tt.HasText("Selected f:old.txt") {
		t.Fatalf("selected %q, frame %q", w.treeSel, tt.Texts())
	}
	if !tt.Focused("Changed files") {
		t.Fatal("the file tree did not take focus on press")
	}
	pixel := func(r ui.Rect) color.RGBA {
		return tt.Image().RGBAAt(int(r.X+r.W-4), int(r.Y+r.H/2))
	}
	want := color.RGBA{R: accent.R, G: accent.G, B: accent.B, A: 255}
	if pixel(old) == want || pixel(r) != want {
		t.Fatalf("old fill %v, new fill %v, want only the new file to have %v", pixel(old), pixel(r), want)
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "f:old.txt" {
		t.Fatal("release changed the selection again")
	}
}

func TestFileTreeDirectoryDisclosureWaitsForRelease(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	r, ok := tt.Find("docs")
	if !ok {
		t.Fatal("the directory row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.treeSel != "d:docs" || w.closedDirs["d:docs"] {
		t.Fatal("press should select the directory and keep it open")
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
	if !w.closedDirs["d:docs"] {
		t.Fatal("release did not close the directory")
	}
}

func TestHistorySelectsOnPressFromOutsideSidebar(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	w.tab = 1
	tt.Frame()
	if err := tt.Click("Filter history"); err != nil {
		t.Fatal(err)
	}
	r, ok := tt.Find("First commit")
	if !ok {
		t.Fatal("the commit row is missing")
	}
	tt.Press(r.X+r.W/2, r.Y+r.H/2)
	if w.source.kind != sourceCommit || w.commit == nil || w.commit.Subject != "First commit" {
		t.Fatalf("press did not select the commit: %+v", w.source)
	}
	if !tt.Focused("History") {
		t.Fatal("the history did not take focus on press")
	}
	tt.Release(r.X+r.W/2, r.Y+r.H/2)
}

func TestSidebarPositionAndResizing(t *testing.T) {
	w, tt := newTestWindow(t, testRepo(t))
	for _, side := range []string{"right", "left", "right"} {
		s := w.settings
		s.SidebarPosition = side
		w.applySettings(s)
		tt.Frame()
		r, ok := tt.Find("Filter files")
		if !ok || (side == "right") != (r.X > 640) {
			t.Fatalf("sidebar did not move to %s: %+v", side, r)
		}
		before := w.sidebarWidth
		x, dx := before, float32(40)
		if side == "right" {
			x, dx = 1280-before-1, -dx
		}
		tt.Press(x, 600)
		tt.Move(x+dx, 600)
		if w.sidebarWidth != before+40 {
			t.Fatalf("%s sidebar grew to %v, want %v", side, w.sidebarWidth, before+40)
		}
		tt.Move(x, 600)
		tt.Release(x, 600)
		if w.sidebarWidth != before {
			t.Fatalf("%s sidebar did not shrink back: %v", side, w.sidebarWidth)
		}
		if err := tt.Click("Hide sidebar (⌘⇧B)"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		button, ok := tt.Find("Show sidebar (⌘⇧B)")
		if !ok || (side == "right") != (button.X > 640) {
			t.Fatalf("collapsed %s sidebar toggle is on the wrong side: %+v", side, button)
		}
		snapshot(t, tt, "sidebar-"+side+"-collapsed")
		if err := tt.Click("Show sidebar (⌘⇧B)"); err != nil {
			t.Fatal(err)
		}
		tt.Frame()
		if !w.sidebarShown {
			t.Fatalf("%s sidebar did not reopen", side)
		}
	}
	snapshot(t, tt, "sidebar-right")
}
