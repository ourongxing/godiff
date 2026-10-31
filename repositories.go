package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// A native window owns one workspace. Each repository keeps its complete
// review state; only the native host and sidebar layout are shared.
type repositoryWorkspace struct {
	root, active *window
	listViews    [3]*window // expanded History/PR list, independent of the review
	entries      []*repositoryEntry
	discovering  bool
	discovered   bool
	err          string
	nextScan     time.Time
}

type repositoryEntry struct {
	path             string
	root             string
	view             *window // nil for an uninitialized submodule
	collapsed        bool
	historyCollapsed bool
	pullsCollapsed   bool
}

func (e *repositoryEntry) name() string {
	if e.path != "" {
		return e.path
	}
	return filepath.Base(e.root)
}

func (e *repositoryEntry) collapseState(tab int) *bool {
	switch tab {
	case 1:
		return &e.historyCollapsed
	case 2:
		return &e.pullsCollapsed
	default:
		return &e.collapsed
	}
}

func (w *window) hasPullRequests() bool {
	if w.gh.ok() {
		return true
	}
	if w.workspace != nil {
		for _, entry := range w.workspace.entries {
			if entry.view != nil && entry.view.gh.ok() {
				return true
			}
		}
	}
	return false
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

func (w *window) listRepository(tab int) *window {
	if ws := w.workspace; ws != nil {
		if ws.listViews[tab] != nil {
			return ws.listViews[tab]
		}
		return ws.active
	}
	return w
}

func (w *window) activateRepository() {
	ws := w.workspace
	if ws == nil {
		return
	}
	previous := ws.active
	if previous.tab != 0 {
		ws.listViews[previous.tab] = w
		for _, entry := range ws.entries {
			if entry.view == w {
				*entry.collapseState(previous.tab) = false
				break
			}
		}
	}
	w.focusHistory, w.focusPulls = previous.tab == 1, previous.tab == 2
	if previous == w {
		return
	}
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
	if ws.discovering || ws.root.ctx.Err() != nil {
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
			if err != nil {
				ws.err = errorText(err)
				return
			}
			first := !ws.discovered
			ws.discovered, ws.err = true, ""
			present := map[*window]bool{}
			for _, entry := range entries {
				if entry.view != nil {
					present[entry.view] = true
				}
			}
			for _, old := range ws.entries {
				if old.view != nil && !present[old.view] {
					old.view.dispose()
				}
			}
			for tab, view := range ws.listViews {
				if !present[view] {
					ws.listViews[tab] = nil
				}
			}
			ws.entries = entries
			if !present[ws.active] {
				ws.root.activateRepository()
			}
			if first && len(entries) == 1 && ws.root.loadedOnce && len(ws.root.files) == 0 && ws.root.source.kind == sourceWorkingTree {
				ws.root.tab = 1
			}
			for _, view := range fresh {
				view.workspace, view.win = ws, ws.root.win
				view.settings = ws.root.settings
				view.filter = ws.active.filter
				view.load()
				view.loadHistory()
				view.loadUser()
			}
		})
	})
}

func (w *window) checkRepositoryChanges() {
	var views []*window
	w.onMain(func() {
		if w.workspace == nil {
			views = []*window{w}
			return
		}
		ws := w.workspace
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

// repositoryPanels adds compact repository headers to each file tree.
func (w *window) repositoryPanels(c *ui.Context) {
	ws, t := w.workspace, c.Theme()
	ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
		for _, entry := range ws.entries {
			ui.Column(c.Key(entry.root)).FillWidth().Children(func() {
				view := entry.view
				w.repositoryHeader(c, entry, 0)
				if entry.collapsed {
					return
				}
				if view == nil {
					ui.Text(c, "Submodule not initialized").FontSize(11).TextColor(t.TextMuted).Padding(8, 14, 12)
					return
				}
				if view.changeErr != "" {
					ui.Text(c, view.changeErr).FontSize(11).TextColor(t.Danger).Padding(6, 14).MaxLines(3)
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

// repositoryHeader is shared by Files, History and Pull Requests.
func (w *window) repositoryHeader(c *ui.Context, entry *repositoryEntry, tab int) {
	ws, t := w.workspace, c.Theme()
	pal, view := paletteFor(t), entry.view
	state := entry.collapseState(tab)
	collapsed := *state || (tab != 0 && view != w.listRepository(tab))
	name := entry.name()
	header := ui.Row(c).Height(28).Gap(5).Margin(2, 8).Padding(0, 8, 0, 6).Radius(6).MinWidth(0)
	header.Children(func() {
		muted := styleSidebarRow(header, t, false, false)
		toggle := ui.ButtonBase(c).Grow(1).MinWidth(0).Height(28).Gap(5).FocusRing(false).
			Label("Repository " + name).Tooltip(entry.root)
		toggle.Disabled(tab != 0 && view == nil).OnClick(func() {
			*state = !collapsed
			if tab != 0 && !*state {
				ws.listViews[tab] = view
				view.focusHistory, view.focusPulls = tab == 1, tab == 2
			}
		})
		toggle.Children(func() {
			sidebarChevron(c, collapsed, muted)
			ui.Text(c, name).FontSize(12).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
		})
		if view != nil {
			if header.Hovered() || header.FocusWithin() {
				view.repositoryActions(c, name)
			} else if view.branch != "" {
				ui.Row(c).Gap(3).Shrink(1).MaxWidth(100).MinWidth(0).TextColor(muted).Tooltip("Branch " + view.branch).Children(func() {
					ui.Icon(c, iconBranch).FontSize(12).Shrink(0)
					ui.Text(c, view.branch).FontSize(11).SingleLine().Shrink(1).MinWidth(0)
				})
			}
			var count string
			switch tab {
			case 0:
				count = fmt.Sprint(view.visibleFileCount())
			case 1:
				count = fmt.Sprint(len(view.history))
				if view.historyMore {
					count += "+"
				}
			case 2:
				count = "…"
				if !view.gh.ok() {
					count = "—"
				} else if view.pullsLoaded {
					count = fmt.Sprint(len(view.pulls))
				}
			}
			ui.Text(c, count).Font(w.codeFont()).FontSize(10).TextColor(muted).Padding(1, 5).Radius(8).Background(pal.pill).Shrink(0)
		}
	})
}

// Repository actions stay with their header, including when another repository
// is selected or this one's list is collapsed.
func (w *window) repositoryActions(c *ui.Context, name string) {
	busy := w.loading || w.changeBusy || w.commitBusy
	iconButton(c.Key("review"), iconFileDiff, "Review local changes in "+name).Size(22, 22).Disabled(busy).
		OnClick(func() { w.reviewChanges("", "") })
	if w.remoteURL != "" {
		services, url := c.Services(), w.remoteURL
		iconButton(c.Key("remote"), iconOpen, "Open remote repository for "+name).Size(22, 22).
			Tooltip("Open remote repository\n" + url).OnClick(func() { services.OpenURL(url) })
	}
	iconButton(c.Key("refresh"), iconRefresh, "Refresh repository "+name).Size(22, 22).Disabled(busy || w.historyLoading).
		OnClick(w.refreshRepository)
}

// Only one repository list expands at a time. Expanding it does not select a
// review; selecting a commit or pull request does.
func (w *window) repositoryLists(c *ui.Context, tab int) {
	ws := w.workspace
	ui.Column(c).Grow(1).MinHeight(0).Children(func() {
		for _, entry := range ws.entries {
			expanded := entry.view != nil && entry.view == w.listRepository(tab) && !*entry.collapseState(tab)
			section := ui.Column(c.Key(entry.root)).FillWidth().MinHeight(0)
			if expanded {
				section.Grow(1)
			} else {
				section.Shrink(0)
			}
			section.Children(func() {
				w.repositoryHeader(c, entry, tab)
				if expanded {
					switch {
					case tab == 1:
						entry.view.historyView(c)
					case entry.view.gh.ok():
						entry.view.pullsView(c)
					default:
						ui.Column(c).Grow(1).Center().Padding(16).Children(func() {
							ui.Text(c, "This repository has no GitHub remote.").FontSize(12).TextColor(c.Theme().TextMuted).TextAlign(ui.Center)
						})
					}
				}
			})
		}
	})
}

func (w *window) visibleFileCount() int {
	if w.source.kind == sourceWorkingTree {
		paths := map[string]bool{}
		for _, n := range w.treeItems {
			if n.file != nil {
				paths[n.file.Path] = true
			}
		}
		return len(paths)
	}
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
