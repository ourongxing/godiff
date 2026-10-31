package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// A native window owns one workspace. Each repository keeps its complete
// review state; only the native host and sidebar layout are shared.
type repositoryWorkspace struct {
	root, active *window
	entries      []*repositoryEntry
	discovering  bool
	discovered   bool
	err          string
	nextScan     time.Time
	closed       atomic.Bool
}

type repositoryEntry struct {
	path      string
	root      string
	view      *window // nil for an uninitialized submodule
	collapsed bool
}

func (w *window) initRepositories() {
	w.workspace = &repositoryWorkspace{root: w, active: w,
		entries: []*repositoryEntry{{root: w.repo.Root, view: w}}}
}

func (w *window) activeRepository() *window {
	if w.workspace != nil {
		return w.workspace.active
	}
	return w
}

func (w *window) multipleRepositories() bool {
	return w.workspace != nil && len(w.workspace.entries) > 1
}

func (w *window) activateRepository() {
	ws := w.workspace
	if ws == nil || ws.active == w {
		return
	}
	previous := ws.active
	w.sidebarWidth, w.sidebarShown = previous.sidebarWidth, previous.sidebarShown
	w.tab = previous.tab
	ws.active = w
	w.focusedOnce = true
	if w.changed && !w.loading && !w.commitBusy {
		w.load()
	}
	if w.win != nil {
		w.win.SetTitle(windowTitle(w.repo.Root, w.source))
	}
}

// discover runs Git and creates new repository states off the UI thread.
// Existing states survive rediscovery, including drafts and in-flight jobs.
func (ws *repositoryWorkspace) discover() {
	if ws.discovering || ws.closed.Load() {
		return
	}
	ws.discovering = true
	ws.nextScan = time.Now().Add(10 * time.Second)
	known := map[string]*repositoryEntry{}
	for _, entry := range ws.entries {
		known[entry.root] = entry
	}
	ws.root.background(func() {
		children, err := ws.root.repo.Subrepositories()
		entries := []*repositoryEntry{known[ws.root.repo.Root]}
		var fresh []*window
		if err == nil {
			for _, child := range children {
				entry := known[child.Root]
				if entry == nil || (entry.view != nil) != child.Initialized {
					entry = &repositoryEntry{path: child.Path, root: child.Root}
					if child.Initialized {
						entry.view = newWindow(&git.Repo{Root: child.Root}, source{})
						fresh = append(fresh, entry.view)
					}
				}
				entries = append(entries, entry)
			}
		}
		ws.root.update(func() {
			ws.discovering = false
			if ws.closed.Load() {
				return
			}
			if err != nil {
				ws.err = errorText(err)
				return
			}
			first := !ws.discovered
			ws.discovered, ws.err = true, ""
			ws.entries = entries
			present := false
			for _, entry := range entries {
				present = present || entry.view == ws.active
			}
			if !present {
				ws.root.activateRepository()
			}
			if first && len(entries) == 1 && ws.root.loadedOnce && len(ws.root.files) == 0 && ws.root.source.kind == sourceWorkingTree {
				ws.root.tab = 1
			}
			for _, view := range fresh {
				view.workspace, view.win = ws, ws.root.win
				view.settings = ws.root.settings
				view.load()
				view.loadHistory()
				view.loadUser()
			}
		})
	})
}

func (w *window) checkRepositoryChanges() {
	var views []*window
	mygo.RunOnMain(func() {
		if w.workspace == nil {
			views = []*window{w}
			return
		}
		ws := w.workspace
		if ws.closed.Load() {
			return
		}
		for _, entry := range ws.entries {
			if entry.view != nil {
				views = append(views, entry.view)
			}
		}
		if time.Now().After(ws.nextScan) {
			ws.discover()
		}
	})
	for _, view := range views {
		view.checkChanges()
	}
}

// repositoryPanels adds compact repository headers to the existing file tree.
// Committing continues to use the original footer and dedicated commit view.
func (w *window) repositoryPanels(c *ui.Context) {
	ws, t := w.workspace, c.Theme()
	pal := paletteFor(t)
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		for _, entry := range ws.entries {
			ui.Column(c.Key(entry.root)).FillWidth().Children(func() {
				view := entry.view
				name := filepath.Base(entry.root)
				if entry.path != "" {
					name = entry.path
				}
				header := ui.Row(c).Height(28).Gap(5).Margin(2, 8).Padding(0, 8, 0, 6).Radius(6).MinWidth(0)
				header.Children(func() {
					toggle := ui.ButtonBase(c).Size(20, 26).Center().Label("Toggle repository " + name)
					toggle.OnClick(func() { entry.collapsed = !entry.collapsed })
					selectRepo := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(28).Gap(6).FocusRing(false).
						Label("Repository " + name).Tooltip(entry.root).Disabled(view == nil)
					muted := styleSidebarRow(header, t, view == ws.active, selectRepo.Focused())
					toggle.Children(func() {
						glyph := iconChevronDown
						if entry.collapsed {
							glyph = iconChevronRight
						}
						ui.Icon(c, glyph).FontSize(14).TextColor(muted)
					})
					selectRepo.OnClick(func() { view.activateRepository() })
					selectRepo.Children(func() {
						ui.Icon(c, iconRepository).FontSize(15).Shrink(0)
						ui.Text(c, name).FontSize(13).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
					})
					if view != nil {
						if view.branch != "" {
							ui.Row(c).Gap(3).Shrink(1).MaxWidth(100).MinWidth(0).TextColor(muted).Tooltip("Branch " + view.branch).Children(func() {
								ui.Icon(c, iconBranch).FontSize(12).Shrink(0)
								ui.Text(c, view.branch).FontSize(11).SingleLine().Shrink(1).MinWidth(0)
							})
						}
						ui.Text(c, fmt.Sprint(view.visibleFileCount())).FontSize(10).TextColor(muted).Padding(1, 5).Radius(8).Background(pal.pill)
					}
				})
				if entry.collapsed {
					return
				}
				if view == nil {
					ui.Text(c, "Submodule not initialized").FontSize(11).TextColor(t.TextMuted).Padding(8, 14, 12)
					return
				}
				if view.loadErr != nil {
					ui.Text(c, errorText(view.loadErr)).FontSize(11).TextColor(t.Danger).Padding(6, 14).MaxLines(3)
				} else if view.loading && len(view.files) == 0 {
					ui.Text(c, "Loading…").FontSize(11).TextColor(t.TextMuted).Padding(6, 14)
				} else {
					height := max(float32(len(view.visibleTree()))*29+4, 42)
					ui.Column(c).Height(height).Children(func() { view.fileTree(c) })
				}
				ui.Box(c).Height(8)
			})
		}
	})
}

func (w *window) visibleFileCount() int {
	count := 0
	for i := range w.files {
		if w.fileVisible(i) {
			count++
		}
	}
	return count
}

func (w *window) repositoryComments() string {
	if w.workspace == nil || len(w.workspace.entries) == 1 {
		return w.commentsMarkdown()
	}
	var parts []string
	for _, entry := range w.workspace.entries {
		if entry.view != nil {
			if comments := entry.view.commentsMarkdown(); comments != "" {
				parts = append(parts, "## "+filepath.Base(entry.root)+"\n\n"+comments)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}
