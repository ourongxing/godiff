package main

import (
	"fmt"
	"log"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// treeNode is a row of the file tree: a file, or a directory with the
// chain of directories holding only it compacted into one row, as
// src/app/components.
type treeNode struct {
	name     string
	dir      bool
	file     *fileState
	path     string // a directory's
	group    *fileGroup
	scope    string // staged or unstaged for local changes
	section  bool
	children []string
}

// The sidebar's width: its default, its bounds, and the width below which
// dragging hides it.
const (
	sidebarDefault  = 292
	sidebarMin      = 220
	sidebarMax      = 640
	sidebarCollapse = 80
)

// fuzzyMatch reports whether the runes of query appear in s in order.
func fuzzyMatch(s, query string) bool {
	if query == "" {
		return true
	}
	s, query = strings.ToLower(s), strings.ToLower(query)
	i := 0
	for _, r := range s {
		if i < len(query) && r == rune(query[i]) {
			i++
		}
	}
	return i >= len(query)
}

// fileVisible reports whether a file shows: it passes the filter and,
// while finding, holds a match.
func (w *window) fileVisible(i int) bool {
	f := w.files[i]
	if !w.hasContent(f) {
		return false
	}
	if !fuzzyMatch(f.Path, strings.TrimSpace(w.filter)) {
		return false
	}
	if w.finding && strings.TrimSpace(w.query) != "" {
		return w.fileMatches[i]
	}
	return true
}

// hasContent reports whether a file has something to show: files whose
// only changes were whitespace, hidden, have nothing.
func (w *window) hasContent(f *fileState) bool {
	return len(f.Hunks) > 0 || f.Binary || f.Directory || f.TooLarge || f.Note != "" ||
		f.Status != diff.Modified || f.OldPath != f.Path || f.ModeChange != ""
}

// fileNote returns why a file's lines do not show, "" when they do.
func (w *window) fileNote(f *fileState) string {
	switch {
	case f.Note != "":
		return f.Note
	case f.Binary:
		return "Binary file changed."
	case f.TooLarge:
		return "File is too large, so Godiff skipped rendering it."
	case len(f.Hunks) == 0 && f.OldPath != f.Path:
		return "File renamed without changes."
	case len(f.Hunks) == 0 && f.ModeChange != "":
		return "File mode changed: " + f.ModeChange + "."
	case len(f.Hunks) == 0 && f.Status == diff.Added, len(f.Hunks) == 0 && f.Status == diff.Untracked:
		return "Empty file added."
	case len(f.Hunks) == 0 && f.Status == diff.Deleted:
		return "Empty file deleted."
	case len(f.Hunks) == 0:
		return "No changes to show."
	}
	return ""
}

// buildTree lays out the tree of the files that show: under their groups,
// when the files are grouped.
func (w *window) buildTree() {
	type dirNode struct {
		dirs  map[string]*dirNode
		order []string
		files map[string]*fileState
	}
	newDir := func() *dirNode { return &dirNode{dirs: map[string]*dirNode{}, files: map[string]*fileState{}} }
	items := map[string]*treeNode{}
	currentFiles := map[string]*fileState{}
	currentIndex := map[string]int{}
	for i, f := range w.files {
		currentFiles[f.Path], currentIndex[f.Path] = f, i
	}
	tree := func(files []*fileState, prefix, scope string) []string {
		root := newDir()
		for _, f := range files {
			parts := strings.Split(f.Path, "/")
			d := root
			for _, p := range parts[:len(parts)-1] {
				next := d.dirs[p]
				if next == nil {
					next = newDir()
					d.dirs[p] = next
					d.order = append(d.order, "d"+p)
				}
				d = next
			}
			name := parts[len(parts)-1]
			d.files[name] = f
			d.order = append(d.order, "f"+name)
		}
		var walk func(*dirNode, string) []string
		walk = func(d *dirNode, path string) []string {
			var keys []string
			for _, o := range d.order {
				name := o[1:]
				if o[0] == 'f' {
					f := d.files[name]
					key := treeFileKey(scope, f.Path)
					items[key] = &treeNode{name: name, file: f, scope: scope}
					keys = append(keys, key)
					continue
				}
				sub := d.dirs[name]
				label, dir := name, path+name
				for len(sub.order) == 1 && sub.order[0][0] == 'd' {
					child := sub.order[0][1:]
					label += "/" + child
					dir += "/" + child
					sub = sub.dirs[child]
				}
				key := "d:" + prefix + dir
				items[key] = &treeNode{name: label, dir: true, path: dir, scope: scope, children: walk(sub, dir+"/")}
				keys = append(keys, key)
			}
			return keys
		}
		return walk(root, "")
	}
	grouped := func(files []*fileState, prefix, scope string) []string {
		groups := w.groups
		byKind := scope != "" && w.grouping() == groupKind
		if byKind {
			groups = make([]fileGroup, len(kinds))
			for i, k := range kinds {
				groups[i] = fileGroup{key: k.key, label: k.label, summary: k.summary}
			}
		}
		byGroup := make([][]*fileState, len(groups))
		var ungrouped []*fileState
		for _, f := range files {
			if !w.hasContent(f) || !fuzzyMatch(f.Path, strings.TrimSpace(w.filter)) {
				continue
			}
			if w.finding && strings.TrimSpace(w.query) != "" {
				i, ok := currentIndex[f.Path]
				if !ok || !w.fileMatches[i] {
					continue
				}
			}
			gi := f.group
			if byKind {
				gi = kindOf(f.File)
			}
			if gi >= 0 && gi < len(groups) {
				byGroup[gi] = append(byGroup[gi], f)
			} else {
				ungrouped = append(ungrouped, f)
			}
		}
		var roots []string
		for gi, files := range byGroup {
			if len(files) == 0 {
				continue
			}
			g := &groups[gi]
			key := "g:" + prefix + g.key
			items[key] = &treeNode{name: g.label, dir: true, group: g, scope: scope, children: tree(files, prefix+g.key+":", scope)}
			roots = append(roots, key)
		}
		return append(roots, tree(ungrouped, prefix, scope)...)
	}
	var roots []string
	if w.source.kind == sourceWorkingTree {
		for _, section := range []struct {
			scope, name string
			files       []*diff.File
		}{
			{"staged", "Staged changes", w.staged}, {"unstaged", "Changes", w.unstaged},
		} {
			var files []*fileState
			for _, f := range section.files {
				fs := &fileState{File: f, group: -1}
				if current := currentFiles[f.Path]; current != nil {
					fs.group = current.group
					if w.source.ref == section.scope {
						fs = current
					}
				}
				files = append(files, fs)
			}
			prefix := ""
			if section.scope == "staged" {
				prefix = "staged:"
			}
			key := "s:" + section.scope
			items[key] = &treeNode{name: section.name, dir: true, scope: section.scope, section: true, children: grouped(files, prefix, section.scope)}
			roots = append(roots, key)
		}
	} else {
		roots = grouped(w.files, "", "")
	}
	w.treeRoots, w.treeItems = roots, items
}

func treeFileKey(scope, path string) string {
	if scope == "staged" {
		return "f:staged:" + path
	}
	return "f:" + path
}

// statusColor is the color of a file's status letter.
func statusColor(s diff.Status, pal *palette, t *ui.Theme) ui.Color {
	switch s {
	case diff.Added, diff.Untracked:
		return pal.addBar
	case diff.Deleted:
		return pal.delBar
	case diff.Renamed, diff.Copied:
		return t.Accent
	case diff.Conflict:
		return t.Danger
	}
	return ui.Hex("#d4a72c")
}

func statusLetter(f *fileState) string {
	if f.Submodule && f.Status != diff.Conflict {
		return "S"
	}
	switch f.Status {
	case diff.Untracked:
		return "U"
	case diff.Conflict:
		return "!"
	}
	return string(f.Status)
}

// styleSidebarRow styles file selections and group hover states. It returns
// the secondary text color for icons, counts and other details in the row.
func styleSidebarRow(row ui.Element, t *ui.Theme, selected, focused bool) ui.Color {
	text, muted := t.Text, t.TextMuted
	switch {
	case selected && focused:
		row.Background(t.Accent)
		text, muted = t.AccentText, t.AccentText.Alpha(0.8)
	case selected:
		row.Background(ui.RGBA(127, 127, 127, 0.2))
	case row.Hovered():
		row.Background(ui.RGBA(127, 127, 127, 0.08))
	}
	row.TextColor(text)
	return muted
}

// All tree disclosures share a slot and glyph, including repository headers.
func sidebarChevron(c *ui.Context, collapsed bool, color ui.Color) {
	ui.Box(c).Size(14, 14).Center().Shrink(0).Children(func() {
		ic := ui.Icon(c, iconChevronDown).FontSize(12).TextColor(color)
		target := float32(0)
		if collapsed {
			target = -90
		}
		ic.Rotate(ic.Animate("rot", target, 150*time.Millisecond))
	})
}

// compact writes a count briefly: 999, 1.2k, 15k, 1.2m.
func compact(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 10000:
		return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000), ".0") + "k"
	case n < 1000000:
		return fmt.Sprintf("%dk", (n+500)/1000)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", float64(n)/1000000), ".0") + "m"
}

// thousands writes a count with separators: 1,234.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func lines(n int, what string) string {
	if n == 1 {
		return "1 " + what + " line"
	}
	return thousands(n) + " " + what + " lines"
}

// countable reports whether a file's lines were counted.
func countable(f *fileState) bool {
	return !f.Binary && !f.Directory && !f.TooLarge
}

func (w *window) sidebar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	sidebarRight := w.settings.SidebarPosition == "right"
	left, right := max(bar.Left+6, 10), float32(10)
	if sidebarRight {
		left, right = 10, max(bar.Right+6, 10)
	}
	ui.Column(c).Width(w.sidebarWidth).Shrink(0).Background(w.sidebarBg(t)).Children(func() {
		// The title bar, under the window controls: the views of the
		// sidebar and its toggle, centered on the window controls. They
		// keep to the right, clear of the traffic lights, on macOS, and
		// to the left elsewhere, where the window buttons are on the right.
		mac := runtime.GOOS == "darwin"
		ui.Row(c).Height(titleBarHeight).Padding(0, right, 0, left).Gap(6).DragWindow().Children(func() {
			if mac {
				ui.Spacer(c)
			}
			if w.tabControl(c) {
				w.commitOpen = false
			}
			w.sidebarToggle(c)
			if !mac {
				ui.Spacer(c)
			}
		})
		if w.tab == 2 && !w.hasPullRequests() {
			w.tab = 0
		}
		ui.Column(c).Padding(2, 10, 8).Children(func() {
			switch w.tab {
			case 0:
				if searchInput(c, &w.filter, "Filter files", &w.filterFocus, &w.typing) {
					w.matchesFor = "\x00"
					w.buildTree()
					w.rowsDirty = true
					if w.multipleRepositories() {
						for _, entry := range w.workspace.entries {
							if entry.view != nil && entry.view != w {
								entry.view.filter = w.filter
								entry.view.buildTree()
								entry.view.rowsDirty = true
							}
						}
					}
				}
			case 1:
				searchInput(c, &w.listRepository(1).historyFilter, "Filter history", &w.filterFocus, &w.typing)
			default:
				searchInput(c, &w.listRepository(2).pullsFilter, "Filter pull requests", &w.filterFocus, &w.typing)
			}
		})
		if w.workspace != nil && w.workspace.err != "" {
			ui.Text(c, "Unable to read subrepositories: "+w.workspace.err).FontSize(11).TextColor(t.Danger).Padding(4, 12).MaxLines(2)
		}
		if w.changeErr != "" && !w.multipleRepositories() {
			ui.Text(c, w.changeErr).FontSize(11).TextColor(t.Danger).Padding(4, 12).MaxLines(3)
		}
		switch w.tab {
		case 0:
			if w.multipleRepositories() {
				w.repositoryPanels(c)
			} else {
				w.fileTree(c)
			}
		default:
			if w.multipleRepositories() {
				w.repositoryLists(c, w.tab)
			} else if w.tab == 1 {
				w.historyView(c)
			} else {
				w.pullsView(c)
			}
		}
	})
}

// sidebarBg uses Dark Modern's solid sidebar in dark mode. Light mode
// keeps the window's material on macOS.
func (w *window) sidebarBg(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.Hex("#181818")
	}
	if runtime.GOOS == "darwin" && w.win != nil {
		return ui.Transparent
	}
	return ui.Hex("#ebebed")
}

// titleBarHeight is the height of the bars along the top, which centers
// their controls on the traffic lights of a window with an inset title bar.
const titleBarHeight = 52

// tabControl switches the sidebar between files and history, and reports a switch.
func (w *window) tabControl(c *ui.Context) bool {
	t := c.Theme()
	pal := paletteFor(t)
	type item struct {
		icon *ui.SVG
		name string
	}
	items := []item{{iconTree, "Files (⌘1)"}, {iconHistory, "History (⌘2)"}}
	// Keep PR's index valid for menu and command actions while its tab is hidden.
	seg := ui.SegmentedBase(c, &w.tab, 3)
	seg.Track.Padding(2).Gap(2).Radius(8).Background(ui.RGBA(127, 127, 127, 0.12)).Label("Sidebar").Children(func() {
		for i, it := range items {
			s := seg.Segment(i).Size(30, 24).Radius(6).Center().Label(it.name).Tooltip(it.name).TextColor(t.TextMuted)
			if i == w.tab {
				s.Background(pal.headerBg).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12)).TextColor(t.Text)
			}
			s.Children(func() { ui.Icon(c, it.icon).FontSize(15) })
		}
	})
	return seg.Track.Changed()
}

// sidebarToggle shows and hides the sidebar.
func (w *window) sidebarToggle(c *ui.Context) {
	tip := "Hide sidebar (⌘⇧B)"
	if !w.sidebarShown {
		tip = "Show sidebar (⌘⇧B)"
	}
	glyph := iconSidebar
	if w.settings.SidebarPosition == "right" {
		glyph = iconSidebarRight
	}
	if iconButton(c, glyph, tip).Clicked() {
		w.toggleSidebar()
	}
}

func (w *window) toggleSidebar() {
	w.sidebarShown = !w.sidebarShown
	if w.sidebarShown && w.sidebarWidth < sidebarMin {
		w.sidebarWidth = sidebarDefault
	}
	go state.setLayout(w.sidebarWidth, w.sidebarShown)
}

// searchInput is a field filtering a list, as the search fields of
// macOS: a magnifying glass, the text, and a button clearing it, as Escape
// does. It reports a change.
func searchInput(c *ui.Context, query *string, placeholder string, focus *bool, typing *bool) bool {
	t := c.Theme()
	changed := false
	box := ui.Row(c).Height(28).Padding(0, 6, 0, 8).Gap(6).Radius(7).Background(ui.RGBA(127, 127, 127, 0.12))
	box.Children(func() {
		ui.Icon(c, iconSearch).FontSize(13).TextColor(t.TextMuted)
		in := ui.TextInputBase(c, query).Placeholder(placeholder).Label(placeholder).FontSize(13).Grow(1).MinWidth(0)
		if *focus {
			in.Focus()
			*focus = false
		}
		if in.Changed() {
			changed = true
		}
		if in.Focused() {
			box.Shadow(0, 0, 0, 3, t.Focus.Alpha(0.45))
			*typing = true
		}
		if *query != "" {
			clear := ui.ButtonBase(c).Size(16, 16).Radius(8).Center().Background(t.TextMuted.Alpha(0.45)).Label("Clear").FocusRing(false)
			clear.Children(func() { ui.Icon(c, iconClose).FontSize(10).TextColor(t.Background) })
			if clear.Clicked() || in.Shortcut(0, ui.KeyEscape) {
				*query = ""
				changed = true
			}
		}
	})
	return changed
}

// iconButton is a button showing an icon alone, as in a toolbar.
func iconButton(c *ui.Context, svg *ui.SVG, tip string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(28, 28).Radius(7).Center().Label(tip).Tooltip(tip).TextColor(t.TextMuted)
	if b.Pressed() {
		b.Background(ui.RGBA(127, 127, 127, 0.22))
	} else if b.Hovered() {
		b.Background(ui.RGBA(127, 127, 127, 0.13))
	}
	b.Children(func() { ui.Icon(c, svg).FontSize(16) })
	return b
}

// treeRow is a row of the file tree as it shows: an item and its depth.
type treeRow struct {
	key   string
	depth int
}

// visibleTree lists the rows of the tree, inside the directories open.
func (w *window) visibleTree() []treeRow {
	var rows []treeRow
	var walk func(keys []string, depth int)
	walk = func(keys []string, depth int) {
		for _, k := range keys {
			rows = append(rows, treeRow{k, depth})
			if n := w.treeItems[k]; n != nil && n.dir && !w.closedDirs[k] {
				walk(n.children, depth+1)
			}
		}
	}
	walk(w.treeRoots, 0)
	return rows
}

func (w *window) fileTree(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	if len(w.treeRoots) == 0 {
		ui.Column(c).Grow(1).Center().Padding(16).Children(func() {
			msg := "No changed files"
			switch {
			case w.loading && len(w.files) == 0:
				msg = "Loading…"
			case strings.TrimSpace(w.filter) != "":
				msg = "No matching files"
			}
			ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	rows := w.visibleTree()
	choose := func(key string) {
		if n := w.treeItems[key]; n != nil && n.section {
			w.treeSel = key
			return
		}
		w.activateRepository()
		w.tab = 0
		w.treeSel = key
		if n := w.treeItems[key]; n != nil && !n.dir {
			w.commitOpen = false
			if n.scope != "" {
				w.reviewChanges(n.scope, n.file.Path)
			} else if i := slices.Index(w.files, n.file); i >= 0 {
				w.revealFile(i)
			}
		}
	}
	w.treeList.Key = func(i int) any { return rows[i].key }
	w.treeList.Label = func(i int) string {
		if n := w.treeItems[rows[i].key]; n != nil {
			return n.name
		}
		return ""
	}
	ui.List(c, &w.treeList, len(rows), func(i int) {
		focused := w.treeList.FocusWithin(c)
		key := rows[i].key
		n := w.treeItems[key]
		if n == nil {
			return
		}
		selected := !n.section && key == w.treeSel
		depth := rows[i].depth
		if w.multipleRepositories() {
			depth++
		}
		row := ui.Row(c).Height(28).Padding(0, 8, 0, 6+float32(depth)*14).Gap(5).Radius(6).MinWidth(0)
		if n.file != nil && n.scope != "" {
			label := "Changed file "
			if n.scope == "staged" {
				label = "Staged file "
			}
			row.Label(label + n.file.Path)
		}
		muted := styleSidebarRow(row, t, selected, focused)
		if !n.section && row.Pressed() && (!selected || w.commitOpen) {
			choose(key)
		}
		if !n.section && row.Clicked() {
			if n.dir {
				w.closedDirs[key] = !w.closedDirs[key]
			}
			choose(key)
		}
		row.Children(func() {
			if n.section {
				w.changeSection(c, key, n, muted)
				return
			}
			if n.dir {
				sidebarChevron(c, w.closedDirs[key], muted)
				if n.group != nil {
					g := n.group
					icon := ui.Icon(c, groupIcon(g)).FontSize(14).TextColor(muted)
					if g.critical && !(selected && focused) {
						icon.TextColor(t.Danger)
					}
					ui.Text(c, n.name).FontSize(13).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0).Tooltip(g.summary)
					ui.Textf(c, "%d", w.treeCount(key)).Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(muted).Shrink(0)
					return
				}
				folderIcon(c, n.name, !w.closedDirs[key], 14)
				ui.Text(c, n.name).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
				return
			}
			ui.Box(c).Size(14, 14).Shrink(0)
			f := n.file
			viewed := w.isViewed(f)
			fileIcon(c, f, 14)
			name := ui.Text(c, n.name).FontSize(13).SingleLine().Grow(1).Shrink(1).MinWidth(0)
			if viewed && !(selected && focused) {
				name.TextColor(t.TextMuted)
			}
			if notes := w.notes[f.Path]; len(notes) > 0 && (n.scope == "" || w.source.ref == "" || n.scope == w.source.ref) {
				// The file has notes of the agent's review.
				color := t.Accent
				if slices.ContainsFunc(notes, func(n *aiNote) bool { return n.critical }) {
					color = t.Danger
				}
				if selected && focused {
					color = t.AccentText
				}
				ui.Icon(c, iconSparkle).FontSize(11).TextColor(color).Shrink(0).Tooltip(plural(len(notes), "AI note"))
			}
			if countable(f) && (f.Additions > 0 || f.Deletions > 0) && !(n.scope != "" && row.Hovered()) {
				ui.Textf(c, "+%s -%s", compact(f.Additions), compact(f.Deletions)).
					Font(w.codeFont()).FontSize(10).FontWeight(600).TextColor(muted).Shrink(0).
					Tooltip(lines(f.Additions, "added") + ", " + lines(f.Deletions, "removed"))
			}
			letter := statusColor(f.Status, pal, t)
			switch {
			case selected && focused:
				letter = t.AccentText
			case viewed:
				letter = t.TextMuted
			case w.reloaded[f.Path]:
				letter = pal.ref
			}
			ui.Text(c, statusLetter(f)).Font(w.codeFont()).FontSize(11).FontWeight(700).TextColor(letter).
				Width(12).TextAlign(ui.Center).Shrink(0).Tooltip(f.Status.Label())
			if n.scope != "" && row.Hovered() {
				w.changeFileActions(c, n)
			}
		})
	}).Grow(1).Padding(2, 8).Gap(1).Focusable().FocusRing(false).Label("Changed files")

	// The keys move the choice, open and close directories.
	at := -1
	for i, r := range rows {
		if r.key == w.treeSel {
			at = i
		}
	}
	move := func(i int) {
		if i >= 0 && i < len(rows) {
			choose(rows[i].key)
			w.treeList.ScrollIntoView(i)
		}
	}
	if w.treeList.Shortcut(c, 0, ui.KeyDown) {
		move(at + 1)
	}
	if w.treeList.Shortcut(c, 0, ui.KeyUp) {
		move(max(at-1, 0))
	}
	if w.treeList.Shortcut(c, 0, ui.KeyHome) {
		move(0)
	}
	if w.treeList.Shortcut(c, 0, ui.KeyEnd) {
		move(len(rows) - 1)
	}
	if at >= 0 {
		n := w.treeItems[rows[at].key]
		if w.treeList.Shortcut(c, 0, ui.KeyRight) && n != nil && n.dir {
			if w.closedDirs[rows[at].key] {
				w.closedDirs[rows[at].key] = false
			} else {
				move(at + 1)
			}
		}
		if w.treeList.Shortcut(c, 0, ui.KeyLeft) {
			if n != nil && n.dir && !w.closedDirs[rows[at].key] {
				w.closedDirs[rows[at].key] = true
			} else {
				// To the directory holding it.
				for j := at - 1; j >= 0; j-- {
					if rows[j].depth < rows[at].depth {
						move(j)
						break
					}
				}
			}
		}
	}
}

func (w *window) changeSection(c *ui.Context, key string, n *treeNode, muted ui.Color) {
	staged := n.scope == "staged"
	files := w.unstaged
	if staged {
		files = w.staged
	}
	toggle := ui.ButtonBase(c).Height(28).Grow(1).MinWidth(0).Gap(5).FocusRing(false).Label(n.name)
	toggle.OnClick(func() { w.closedDirs[key] = !w.closedDirs[key] })
	toggle.Children(func() {
		sidebarChevron(c, w.closedDirs[key], muted)
		ui.Text(c, n.name).FontSize(12).FontWeight(600).SingleLine().Grow(1).Shrink(1).MinWidth(0)
	})
	busy := w.changeBusy || w.commitBusy || w.loading
	if iconButton(c, iconFileDiff, "Review "+strings.ToLower(n.name)).Size(22, 22).Disabled(len(files) == 0 || busy).Clicked() {
		w.reviewChanges(n.scope, "")
	}
	if !staged {
		canDiscard := slices.ContainsFunc(files, git.CanDiscard)
		if iconButton(c, iconUndo, "Discard all changes").Size(22, 22).Disabled(!canDiscard || busy).Clicked() {
			w.askDiscard(files)
		}
	}
	glyph, tip := iconPlus, "Stage all changes"
	if staged {
		glyph, tip = iconMinus, "Unstage all changes"
	}
	if iconButton(c, glyph, tip).Size(22, 22).Disabled(len(changePaths(files)) == 0 || busy).Clicked() {
		w.changeIndex(staged, files)
	}
	ui.Textf(c, "%d", len(files)).Font(w.codeFont()).FontSize(10).TextColor(muted).Padding(1, 5).Radius(8).Background(paletteFor(c.Theme()).pill).Shrink(0)
}

func (w *window) changeFileActions(c *ui.Context, n *treeNode) {
	f := n.file.File
	files := []*diff.File{f}
	busy := w.changeBusy || w.commitBusy || w.loading
	if n.scope == "unstaged" && git.CanDiscard(f) {
		if iconButton(c, iconUndo, "Discard changes to "+f.Path).Size(20, 22).Disabled(busy).Clicked() {
			w.askDiscard(files)
		}
	}
	glyph, tip := iconPlus, "Stage "+f.Path
	staged := n.scope == "staged"
	if staged {
		glyph, tip = iconMinus, "Unstage "+f.Path
	}
	if iconButton(c, glyph, tip).Size(20, 22).Disabled(busy || len(changePaths(files)) == 0).Clicked() {
		w.changeIndex(staged, files)
	}
}

// treeCount counts the files under a row of the tree.
func (w *window) treeCount(key string) int {
	n := w.treeItems[key]
	if n == nil {
		return 0
	}
	if !n.dir {
		return 1
	}
	total := 0
	for _, k := range n.children {
		total += w.treeCount(k)
	}
	return total
}

// selectTreeFile chooses a file's row in the tree, as the surface
// scrolls to it.
func (w *window) selectTreeFile(i int) {
	scope := ""
	if w.source.kind == sourceWorkingTree {
		scope = w.source.ref
		if scope == "" {
			scope = "unstaged"
			if w.treeItems[treeFileKey(scope, w.files[i].Path)] == nil {
				scope = "staged"
			}
		}
	}
	key := treeFileKey(scope, w.files[i].Path)
	if w.treeSel == key {
		return
	}
	w.treeSel = key
	// The directories holding it open, and its group.
	for k, n := range w.treeItems {
		if !n.dir || !w.closedDirs[k] {
			continue
		}
		if n.scope != scope {
			continue
		}
		if n.section {
			w.closedDirs[k] = false
		}
		sameGroup := n.group != nil && w.files[i].group >= 0 && w.files[i].group < len(w.groups) && n.group.key == w.groups[w.files[i].group].key
		if sameGroup || (n.path != "" && strings.HasPrefix(w.files[i].Path, n.path+"/")) {
			w.closedDirs[k] = false
		}
	}
	for r, row := range w.visibleTree() {
		if row.key == key {
			w.treeList.ScrollIntoView(r)
			return
		}
	}
}

// historyView lists the commits of the History tab.
func (w *window) historyView(c *ui.Context) {
	t := c.Theme()
	pal := paletteFor(t)
	q := strings.ToLower(strings.TrimSpace(w.historyFilter))
	type entry struct {
		local  bool
		commit *git.Commit
	}
	var entries []entry
	if q == "" {
		entries = append(entries, entry{local: true})
	}
	for i := range w.history {
		cm := &w.history[i]
		if q == "" || strings.Contains(strings.ToLower(cm.Subject), q) || strings.HasPrefix(cm.Hash, q) || strings.Contains(strings.ToLower(cm.Author), q) {
			entries = append(entries, entry{commit: cm})
		}
	}
	target := w.source
	current := -1
	for i, e := range entries {
		if (e.local && (target.kind == sourceWorkingTree || target.kind == sourceBranch)) || (e.commit != nil && target.kind == sourceCommit && target.ref == e.commit.Hash) {
			current = i
		}
	}
	open := func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		w.activateRepository()
		w.commitOpen = false
		w.historyList.ScrollIntoView(i)
		if entries[i].local {
			w.setSource(w.launchWorkTree())
		} else {
			w.setSource(source{kind: sourceCommit, ref: entries[i].commit.Hash})
		}
	}
	w.historyList.Key = func(i int) any {
		if entries[i].local {
			return "local"
		}
		return entries[i].commit.Hash
	}
	w.historyList.Label = func(i int) string {
		if entries[i].local {
			return "Uncommitted changes"
		}
		return entries[i].commit.Subject
	}
	now := time.Now()
	list := ui.List(c, &w.historyList, len(entries), func(i int) {
		focused := w.historyList.FocusWithin(c) || w.focusHistory
		e := entries[i]
		row := ui.Row(c).Gap(8).Padding(5, 8).Radius(6).AlignItems(ui.Start).Role(ui.RoleButton)
		if e.local {
			row.Label("Uncommitted changes")
		} else {
			row.Label(e.commit.Subject)
		}
		muted, ref := t.TextMuted, pal.ref
		switch {
		case i == current && focused && w.activeRepository() == w:
			row.Background(t.Accent).TextColor(t.AccentText)
			muted, ref = t.AccentText.Alpha(0.75), t.AccentText
		case i == current && w.activeRepository() == w:
			row.Background(ui.RGBA(127, 127, 127, 0.2))
		case row.Hovered():
			row.Background(ui.RGBA(127, 127, 127, 0.08))
		}
		if (row.Pressed() || row.Clicked()) && (i != current || w.activeRepository() != w) {
			if debugFrames {
				log.Printf("history row %d clicked", i)
			}
			open(i)
		}
		row.Children(func() {
			if e.local {
				ui.Text(c, "local").Font(w.codeFont()).FontSize(12).TextColor(ref).Width(56).Shrink(0)
				ui.Text(c, "Uncommitted changes").FontSize(12).SingleLine().Grow(1)
				return
			}
			cm := e.commit
			when := w.commitTimes[cm.Hash]
			if when.at != now.Truncate(time.Minute) {
				// Formatted once a minute, rather than every frame.
				when = commitTime{at: now.Truncate(time.Minute), ago: relativeTime(now, cm.Time), full: cm.Time.Format("Mon Jan 2 15:04:05 2006")}
				w.commitTimes[cm.Hash] = when
			}
			ui.Text(c, cm.Short).Font(w.codeFont()).FontSize(12).TextColor(ref).Width(56).Shrink(0)
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, cm.Subject).FontSize(12).SingleLine().Tooltip(cm.Subject)
				ui.Row(c).Gap(6).Children(func() {
					ui.Text(c, cm.Author).FontSize(10).SingleLine().TextColor(muted).Grow(1).MinWidth(0)
					ui.Text(c, when.ago).FontSize(10).TextColor(muted).Shrink(0).Tooltip(when.full)
				})
			})
		})
	}).Grow(1).Padding(2, 8).Gap(1).Focusable().FocusRing(false).Label("History")
	if w.focusHistory && w.historyList.Focus(c) {
		w.focusHistory = false
	}
	list.Children(func() {
		if len(entries) == 0 {
			ui.Text(c, "No matching commits").FontSize(12).TextColor(t.TextMuted).Padding(12)
		}
	})
	if w.historyList.Shortcut(c, 0, ui.KeyDown) {
		open(current + 1)
	}
	if w.historyList.Shortcut(c, 0, ui.KeyUp) {
		open(max(current-1, 0))
	}
	// More commits load well before the end comes into view, so that
	// scrolling does not stop there.
	if q == "" && w.historyMore && !w.historyLoading {
		if _, last := w.historyList.Visible(); last >= len(entries)-historyPage/2 {
			w.historyLimit += historyPage
			w.loadHistory()
		}
	}
}

// commitTime is how a commit's time shows, as of a minute.
type commitTime struct {
	at        time.Time
	ago, full string
}

// launchWorkTree is the source of uncommitted changes: the work tree, or
// its comparison with a branch when the window was opened on one.
func (w *window) launchWorkTree() source {
	if w.launch.kind == sourceBranch {
		return w.launch
	}
	return source{kind: sourceWorkingTree}
}

// relativeTime writes how long ago t was: just now, 5m ago, 3d ago; ""
// for no time.
func relativeTime(now, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d/(30*24*time.Hour)))
	}
	return fmt.Sprintf("%dy ago", int(d/(365*24*time.Hour)))
}

// sidebarResizer is the edge of the sidebar the user drags.
func (w *window) sidebarResizer(c *ui.Context, pal *palette) {
	ui.Box(c).Width(1).Shrink(0).Background(pal.cardBorder).Children(func() {
		handle := ui.Box(c).Absolute().Top(0).Bottom(0).Left(-4).Width(9).Cursor(ui.CursorResizeEW)
		if dx, _, held := handle.Dragged(); held {
			if w.settings.SidebarPosition == "right" {
				dx = -dx
			}
			w.dragWidth += dx
			switch {
			case w.dragWidth < sidebarCollapse:
				w.sidebarShown = false
			default:
				w.sidebarShown = true
				w.sidebarWidth = min(max(w.dragWidth, sidebarMin), sidebarMax)
			}
		} else {
			if w.dragWidth != 0 && w.dragWidth != w.sidebarWidth {
				go state.setLayout(w.sidebarWidth, w.sidebarShown)
			}
			w.dragWidth = w.sidebarWidth
		}
	})
}
