package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/godiff/internal/github"
	"github.com/egoist/godiff/internal/highlight"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// sourceKind is what a window reviews.
type sourceKind uint8

const (
	sourceWorkingTree sourceKind = iota // uncommitted changes
	sourceCommit                        // a commit, against its first parent
	sourceBranch                        // the work tree since it branched from a branch
	sourcePull                          // a pull request on GitHub, since it branched
)

type source struct {
	kind sourceKind
	ref  string // the commit, the branch, or the pull request as owner/repo#123
}

// historyPage is how many commits the History tab loads at a time: git
// lists hundreds in a few milliseconds, and the list builds only the rows
// in view.
const historyPage = 200

// session is the review of one source, kept while the window shows
// another.
type session struct {
	comments []*comment
	viewed   map[string]string
}

// window is a window reviewing a repository.
type window struct {
	win       *mygo.Window
	ctx       context.Context
	cancel    context.CancelFunc
	repo      *git.Repo
	workspace *repositoryWorkspace
	launch    source // what the window was opened on
	source    source
	settings  Settings

	branch     string
	remoteURL  string // the current repository's page, read off the UI thread
	files      []*fileState
	commit     *git.Commit // the commit reviewed, for sourceCommit
	base       string      // the merge base, for sourceBranch and sourcePull
	loading    bool
	loadedOnce bool
	loadErr    error
	gen        int // counts loads, to drop the results of older ones
	genA       atomic.Int64
	// loadStart is when the load started.
	loadStart time.Time
	viewed    map[string]string
	// loadStatus says what a slow load does, as fetching a pull request.
	loadStatus string
	// Both local change lists stay visible while either diff is reviewed.
	staged, unstaged []*diff.File
	changeBusy       bool
	changeErr        string
	discardChanges   []*diff.File
	pendingFile      string

	// The sidebar.
	sidebarShown bool
	sidebarWidth float32
	tab          int // 0 files, 1 history, 2 pull requests
	filter       string
	filterFocus  bool
	treeList     ui.ListState
	treeSel      string // the key of the row chosen
	closedDirs   map[string]bool
	treeItems    map[string]*treeNode
	treeRoots    []string
	history      []git.Commit
	historyLimit int
	historyMore  bool
	historyGen   int
	historyList  ui.ListState
	commitTimes  map[string]commitTime
	// historyFilter filters the History tab, apart from the files'.
	historyFilter  string
	historyLoading bool
	// reloaded are the files that changed in the last refresh.
	reloaded  map[string]bool
	dragWidth float32

	// The diff surface.
	rows       []row
	rowsDirty  bool
	list       ui.ListState
	current    int // the file shown at the top of the surface
	hscroll    map[string]float32
	revealedAt time.Time
	now        time.Time
	charW      float32
	charKey    string
	// The hunk chosen with j and k.
	selFile, selHunk int
	focusList        bool
	focusedOnce      bool
	focusHistory     bool
	focusPulls       bool
	switchedAt       time.Time // when the source last changed, for GODIFF_DEBUG
	debugPressed     bool
	// hold keeps the background work of tests in held, to run frames
	// while it waits.
	hold bool
	held []func()
	// typing is set while a field has the focus, whose keys are its own.
	typing bool

	// Find in diffs.
	finding     bool
	query       string
	matches     []match
	match       int
	matchesFor  string
	fileMatches map[int]bool

	// The reviews of the other sources.
	sessions map[source]*session

	// GitHub: the repository's remote there, its open pull requests, and
	// the pull requests reviewed, the one shown in pr.
	gh           ghRemote
	pulls        []github.Summary
	pullsLoaded  bool
	pullsLoading bool
	pullsErr     error
	pullsList    ui.ListState
	pullsFilter  string
	pr           *pullState
	prs          map[source]*pullState

	// The groups of the files: by kind, or as an agent's review has them.
	groupPref groupMode
	groups    []fileGroup
	// notes are the notes of the agent's review, by path, and noteSel the
	// one gone to last.
	notes   map[string][]*aiNote
	noteSel *aiNote
	// regroupPending groups the files again as the next frame starts.
	regroupPending bool
	// placed are the threads and notes the rows show under their lines,
	// as the rows are built: the others show at the end of their files.
	placed map[any]bool
	// The agents' reviews, by source.
	analyses map[source]*analysisState
	// Review comments, the one asked about discarding, and the git user.
	comments   []*comment
	discarding *comment
	user       string
	copied     string
	copiedAt   time.Time

	// Committing.
	commitOpen     bool
	subject        string
	body           string
	commitPaths    map[string]bool
	commitBusy     bool
	commitErr      string
	commitOutput   string
	commitDone     string // the hash committed
	committedFiles int
	commitFocus    bool

	// The command bar.
	paletteOpen  bool
	paletteQuery string
	paletteRow   int
	paletteList  ui.ListState
	// palettePointer is where the pointer was over the list.
	palettePointer [2]float32

	// The dialog opening a commit or a branch.
	dialog      dialogKind
	dialogOpen  bool
	dialogValue string
	dialogErr   string
	dialogBusy  bool

	// changed is set when the work tree changed since the last load.
	changed   bool
	signature string
	help      bool
}

var (
	windowsMu sync.Mutex
	windows   []*window
)

// openWindow opens a window on the repository holding dir.
func openWindow(dir string, src source) error {
	repo, err := git.Open(dir)
	if err != nil {
		return err
	}
	if src.kind == sourceCommit {
		hash, err := repo.Resolve(src.ref)
		if err != nil {
			return err
		}
		src.ref = hash
	}
	if src.kind == sourcePull {
		ref, err := resolvePull(repo, src.ref)
		if err != nil {
			return err
		}
		src.ref = ref
	}
	// A repository already open comes to the front.
	windowsMu.Lock()
	for _, w := range windows {
		if w.repo.Root == repo.Root && w.launch == src {
			windowsMu.Unlock()
			w.win.Show()
			w.win.Focus()
			return nil
		}
	}
	windowsMu.Unlock()

	w := newWindow(repo, src)
	w.initRepositories()
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title:          windowTitle(repo.Root, src),
		Width:          1280,
		Height:         860,
		MinWidth:       720,
		MinHeight:      420,
		StateKey:       "main",
		TitleBarStyle:  mygo.TitleBarHiddenInset,
		TitleBarHeight: 52,
		Vibrancy:       mygo.VibrancySidebar,
		Content:        ui.View(w.view),
	})
	windowsMu.Lock()
	windows = append(windows, w)
	windowsMu.Unlock()
	stop := make(chan struct{})
	w.win.OnClose(func(*mygo.CloseEvent) {
		if comments := w.repositoryComments(); w.settings.CopyCommentsOnClose && comments != "" {
			mygo.Clipboard.WriteText(comments)
		}
	})
	offSettings := cfg.OnChange(func(s Settings) {
		w.win.Update(func() {
			for _, entry := range w.workspace.entries {
				if entry.view != nil {
					entry.view.applySettings(s)
				}
			}
		})
	})
	w.win.OnClosed(func() {
		close(stop)
		for _, entry := range w.workspace.entries {
			if entry.view != nil {
				entry.view.dispose()
			}
		}
		offSettings()
		windowsMu.Lock()
		for i, o := range windows {
			if o == w {
				windows = append(windows[:i], windows[i+1:]...)
				break
			}
		}
		windowsMu.Unlock()
	})
	w.win.OnFocus(func() { go w.checkRepositoryChanges() })
	go noteRecent(repo.Root)
	w.load()
	w.loadHistory()
	w.loadUser()
	w.workspace.discover()
	go w.watch(stop)
	w.captureIfAsked()
	return nil
}

// newWindow makes the state of a window reviewing a source of a
// repository.
func newWindow(repo *git.Repo, src source) *window {
	width, shown := state.layout()
	ctx, cancel := context.WithCancel(context.Background())
	w := &window{
		ctx:          ctx,
		cancel:       cancel,
		repo:         repo,
		launch:       src,
		source:       src,
		settings:     cfg.Get(),
		sidebarShown: shown,
		sidebarWidth: width,
		historyLimit: historyPage,
		viewed:       state.viewed(viewedKey(repo.Root, src)),
		hscroll:      map[string]float32{},
		closedDirs:   map[string]bool{},
		commitTimes:  map[string]commitTime{},
		reloaded:     map[string]bool{},
		fileMatches:  map[int]bool{},
		selFile:      -1,
		selHunk:      -1,
		prs:          map[source]*pullState{},
		analyses:     map[source]*analysisState{},
	}
	w.gh, _ = findGitHubRemote(repo, github.Repo{})
	if src.kind == sourcePull {
		w.pr = w.pullFor(src)
	}
	w.loadAnalysis(src)
	w.dragWidth = w.sidebarWidth
	w.list.Key = func(i int) any { return w.key(&w.rows[i]) }
	w.list.Header = func(i int) bool { return w.rows[i].kind == rowHeader }
	return w
}

// windowTitle is "<repository>[/<source>] · Godiff".
func windowTitle(root string, src source) string {
	name := filepath.Base(root)
	switch src.kind {
	case sourceCommit:
		name += "/" + shortHash(src.ref)
	case sourceBranch:
		name += "/" + src.ref
	case sourcePull:
		_, n := src.pull()
		name += fmt.Sprintf("/#%d", n)
	}
	return name + " · Godiff"
}

func shortHash(h string) string {
	if len(h) > 7 {
		return h[:7]
	}
	return h
}

// applySettings takes settings changed in the file or the menus.
func (w *window) applySettings(s Settings) {
	old := w.settings
	w.settings = s
	if old.ShowWhitespace != s.ShowWhitespace {
		w.load()
	}
	if old.DiffStyle != s.DiffStyle || old.WordWrap != s.WordWrap || old.CodeFontSize != s.CodeFontSize {
		w.rowsDirty = true
	}
}

// background runs fn on a goroutine of its own, or at once in tests,
// which have no window.
func (w *window) background(fn func()) {
	work := func() {
		if w.ctx.Err() == nil {
			fn()
		}
	}
	if w.win == nil {
		if w.hold {
			// Tests run frames while the work waits.
			w.held = append(w.held, work)
			return
		}
		work()
		return
	}
	go work()
}

// dispose ends jobs owned by a closed window or a removed repository.
func (w *window) dispose() {
	w.cancel()
	w.genA.Add(1)
}

// onMain reads UI state synchronously, including in headless tests.
func (w *window) onMain(fn func()) {
	read := func() {
		if w.ctx.Err() == nil {
			fn()
		}
	}
	if w.win == nil {
		read()
	} else {
		mygo.RunOnMain(read)
	}
}

// update runs fn on the main thread, then draws a frame.
func (w *window) update(fn func()) {
	apply := func() {
		if w.ctx.Err() != nil {
			return
		}
		if debugFrames {
			start := time.Now()
			defer func() {
				if d := time.Since(start); d > 2*time.Millisecond {
					log.Printf("slow update: %v", d)
				}
			}()
		}
		fn()
	}
	if w.win == nil {
		apply()
	} else {
		w.win.Update(apply)
	}
}

func (w *window) invalidate() {
	if w.win != nil {
		w.win.Invalidate()
	}
}

func (w *window) split() bool { return w.settings.DiffStyle != "unified" }

// splitFile reports whether a file shows side by side: files that are
// all new or all gone show in one column.
func (w *window) splitFile(f *fileState) bool {
	if !w.split() {
		return false
	}
	return !f.oneSided()
}

// setSource shows another source in the window: at once, with what is
// known of it, as the commit's message from the history; its changes come
// as they load.
func (w *window) setSource(src source) {
	w.setSourceFile(src, "")
}

func (w *window) setSourceFile(src source, path string) {
	if w.source == src {
		return
	}
	if debugFrames {
		w.switchedAt = time.Now()
		log.Printf("switch to %+v", src)
		go probeMainThread(time.Second)
	}
	w.commitOpen = false
	// The review takes the focus as the window opens only: the user, who
	// chose this source, keeps the focus where they put it, as on the
	// history.
	w.focusedOnce = true
	w.switchTo(src)
	w.pendingFile = path
	if src.kind == sourceCommit {
		for i := range w.history {
			if w.history[i].Hash == src.ref {
				c := w.history[i]
				w.commit = &c
			}
		}
	}
	w.load()
}

// switchTo makes a source the one shown, its review in place of the
// other's.
func (w *window) switchTo(src source) {
	w.pendingFile = ""
	// Each source keeps its review: the comments, and what was viewed of a
	// commit.
	if w.sessions == nil {
		w.sessions = map[source]*session{}
	}
	w.sessions[w.source] = &session{comments: w.comments, viewed: w.viewed}
	w.comments, w.viewed = nil, nil
	if s := w.sessions[src]; s != nil {
		w.comments, w.viewed = s.comments, s.viewed
	}
	if w.viewed == nil {
		w.viewed = map[string]string{}
		if key := viewedKey(w.repo.Root, src); key != "" {
			w.viewed = state.viewed(key)
		}
	}
	w.source = src
	w.files, w.rows, w.commit, w.groups = nil, nil, nil, nil
	w.pr = nil
	if src.kind == sourcePull {
		w.pr = w.pullFor(src)
	}
	w.loadAnalysis(src)
	// The tree refers to the files by their index: it goes with them.
	w.buildTree()
	w.loadErr = nil
	w.list = ui.ListState{Key: w.list.Key, Header: w.list.Header}
	w.selFile, w.selHunk = -1, -1
	w.finding, w.query = false, ""
	w.hscroll = map[string]float32{}
	w.current = 0
	if w.win != nil {
		w.win.SetTitle(windowTitle(w.repo.Root, src))
	}
}

// preloadBudget is how long after it starts a load may wait for the
// contents of the first files, to show them colored at once rather than
// as they come.
const preloadBudget = 80 * time.Millisecond

// load reads the changes of the source, the contents of the first files
// within preloadBudget, then the contents of the others.
func (w *window) load() {
	w.gen++
	gen := w.gen
	src := w.source
	opts := git.Options{ShowWhitespace: w.settings.ShowWhitespace}
	w.loading = true
	w.loadStart = time.Now()
	started := w.loadStart
	w.loadErr = nil
	w.changed = false
	w.genA.Store(int64(gen))
	// A commit read from history needs no second metadata lookup.
	known := w.commit
	if known != nil && known.Hash != src.ref {
		known = nil
	}
	var branch string
	remote := w.pullRemote(src)
	w.loadStatus = ""
	// Files unchanged since the last load keep their contents.
	loadedFiles := map[string]bool{}
	for _, f := range w.files {
		if f.loaded {
			loadedFiles[f.Path+"\x00"+f.Fingerprint] = true
		}
	}
	w.background(func() {
		var (
			files            []*diff.File
			staged, unstaged []*diff.File
			commit           = known
			base             string
			pulled           pulledRequest
			err              error
			wg               sync.WaitGroup
		)
		wg.Add(1)
		go func() {
			defer wg.Done()
			branch = w.repo.Branch()
		}()
		sig := ""
		switch src.kind {
		case sourceWorkingTree:
			sig = w.repo.StatusSignature()
			staged, err = w.repo.Staged(opts)
			if err == nil {
				unstaged, err = w.repo.Unstaged(opts)
			}
			if err == nil {
				switch src.ref {
				case "staged":
					files = staged
				case "unstaged":
					files = unstaged
				default:
					files, err = w.repo.WorkingTree(opts)
				}
			}
		case sourceCommit:
			if commit == nil {
				var c git.Commit
				c, err = w.repo.CommitInfo(src.ref)
				commit = &c
			}
			if err == nil {
				files, err = w.repo.CommitDiff(*commit, opts)
			}
		case sourceBranch:
			sig = w.repo.StatusSignature()
			files, base, err = w.repo.Compare(src.ref, opts)
		case sourcePull:
			pulled, err = w.fetchPull(gen, src, remote, opts)
			files, base = pulled.files, pulled.mergeBase
		}
		wg.Wait()
		remoteURL := w.repo.WebURL(branch)
		// The revisions of the old and the new side; "" is the work tree.
		var oldRev, newRev string
		switch src.kind {
		case sourceWorkingTree:
			if src.ref == "unstaged" {
				oldRev = ":"
			} else if w.repo.HasHead() {
				oldRev = "HEAD"
			}
			if src.ref == "staged" {
				newRev = ":"
			}
		case sourceCommit:
			if commit != nil && len(commit.Parents) > 0 {
				oldRev = commit.Parents[0]
			}
			newRev = src.ref
		case sourceBranch:
			oldRev = base
		case sourcePull:
			oldRev, newRev = base, pulled.head
		}
		var pre map[string]loaded
		if err == nil && !w.stale(gen) {
			var fresh []*diff.File
			for _, f := range files {
				if !loadedFiles[f.Path+"\x00"+f.Fingerprint] {
					fresh = append(fresh, f)
				}
			}
			if budget := preloadBudget - time.Since(started); budget > 0 {
				pre = w.preload(fresh, oldRev, newRev, budget)
			}
		}
		w.update(func() {
			if gen != w.gen {
				return
			}
			w.loading = false
			w.loadErr = err
			w.loadStatus = ""
			w.branch = branch
			w.remoteURL = remoteURL
			if err == nil {
				w.commit = commit
			}
			if src.kind == sourcePull && err == nil {
				w.pr.apply(pulled)
				w.loadReviews()
			}
			w.base = base
			if sig != "" {
				w.signature = sig
			}
			if src.kind == sourceWorkingTree && err == nil {
				w.staged, w.unstaged = staged, unstaged
			}
			w.setFiles(files)
			if path := w.pendingFile; path != "" {
				w.pendingFile = ""
				for i, f := range w.files {
					if f.Path == path {
						w.revealFile(i)
						break
					}
				}
			}
			for _, f := range w.files {
				if l, ok := pre[f.Path]; ok && !f.loaded {
					l.file = f
					l.apply()
				}
			}
			if !w.loadedOnce && len(files) == 0 && len(staged)+len(unstaged) == 0 && src.kind != sourceCommit && src.kind != sourcePull &&
				!w.multipleRepositories() && (w.workspace == nil || w.workspace.discovered) {
				// Nothing to review: the history shows instead, and takes
				// the keys in place of the review.
				w.tab = 1
				w.focusHistory = w.sidebarShown
				w.focusedOnce = true
			}
			w.loadedOnce = true
			if err == nil {
				var rest []*fileState
				for _, f := range w.files {
					if !f.loaded {
						rest = append(rest, f)
					}
				}
				if len(rest) > 0 {
					w.background(func() { w.loadContents(gen, oldRev, newRev, rest) })
				}
			}
		})
	})
}

// preload reads and colors the contents of files, in order, until
// budget runs out: the files it did not finish load later, as they come.
func (w *window) preload(files []*diff.File, oldRev, newRev string, budget time.Duration) map[string]loaded {
	if len(files) == 0 {
		return nil
	}
	contents, err := w.repo.NewContents()
	if err != nil {
		return nil
	}
	defer boostGC()()
	results := make(chan loaded)
	var stop atomic.Bool
	go func() {
		defer close(results)
		defer contents.Close()
		for _, f := range files {
			if stop.Load() {
				return
			}
			l := w.read(contents, f, oldRev, newRev)
			l.finish()
			select {
			case results <- l:
			case <-time.After(budget):
				return // nobody waits anymore
			}
		}
	}()
	out := map[string]loaded{}
	deadline := time.NewTimer(budget)
	defer deadline.Stop()
	for {
		select {
		case l, ok := <-results:
			if !ok {
				return out
			}
			out[l.path] = l
		case <-deadline.C:
			stop.Store(true)
			return out
		}
	}
}

// setFiles shows newly read files, keeping what the user did to those
// that did not change.
func (w *window) setFiles(files []*diff.File) {
	prev := map[string]*fileState{}
	for _, f := range w.files {
		prev[f.Path] = f
	}
	// A refresh marks the files that changed since, and opens them.
	refresh := len(prev) > 0
	w.reloaded = map[string]bool{}
	next := make([]*fileState, 0, len(files))
	for _, f := range files {
		fs := &fileState{File: f}
		switch p := prev[f.Path]; {
		case p != nil && p.Fingerprint == f.Fingerprint:
			fs.collapsed = p.collapsed
			fs.expanded = p.expanded
			// The contents stay until they load again.
			fs.oldLines, fs.newLines, fs.oldHL, fs.newHL = p.oldLines, p.newLines, p.oldHL, p.newHL
			fs.oldImage, fs.newImage, fs.oldSize, fs.newSize = p.oldImage, p.newImage, p.oldSize, p.newSize
			fs.loaded = p.loaded
		case refresh:
			w.reloaded[f.Path] = true
			fs.collapsed = f.Generated || f.Directory
		default:
			fs.collapsed = w.isViewed(fs) || f.Generated || f.Directory
		}
		next = append(next, fs)
	}
	w.files = next
	w.regroup()
	w.pruneComments()
}

// isViewed reports whether the user marked the file viewed, as it is now.
func (w *window) isViewed(f *fileState) bool {
	return f.Fingerprint != "" && w.viewed[f.Path] == f.Fingerprint
}

func (w *window) setViewed(f *fileState, viewed bool) {
	fp := ""
	if viewed {
		fp = f.Fingerprint
		w.viewed[f.Path] = fp
	} else {
		delete(w.viewed, f.Path)
	}
	// Only the work tree's and pull requests' files are remembered:
	// commits do not change.
	if key := viewedKey(w.repo.Root, w.source); key != "" {
		go state.setViewed(key, f.Path, fp)
	}
	f.collapsed = viewed
	w.rowsDirty = true
}

// maxContent is the size of the files whose contents are loaded.
const maxContent = 2 << 20

// loaded is a file's contents, read and tokenized, or decoded for
// pictures.
type loaded struct {
	file               *fileState // where they go, once known
	oldLines, newLines []string
	oldHL, newHL       [][]highlight.Seg
	oldData, newData   []byte // of pictures
	oldImage, newImage *ui.Bitmap
	path, oldPath      string
}

// read reads both sides of a file: lines of text, or the data of a
// picture.
func (w *window) read(contents *git.Contents, f *diff.File, oldRev, newRev string) loaded {
	l := loaded{path: f.Path, oldPath: f.OldPath}
	raw := func(rev, path string, limit int) []byte {
		var data []byte
		if rev == "" {
			data = w.repo.ReadWorkTree(path)
		} else {
			if rev == ":" {
				rev = "" // cat-file's :path names the index entry.
			}
			data, _ = contents.Read(rev, path)
		}
		if len(data) > limit {
			return nil
		}
		return data
	}
	hasOld := oldRev != "" && f.Status != diff.Added && f.Status != diff.Untracked
	hasNew := f.Status != diff.Deleted
	if f.Binary && isImage(f.Path) {
		if hasOld {
			l.oldData = raw(oldRev, f.OldPath, maxImage)
		}
		if hasNew {
			l.newData = raw(newRev, f.Path, maxImage)
		}
		return l
	}
	if f.Binary || f.Directory || f.TooLarge || f.Submodule {
		return l
	}
	text := func(data []byte) []string {
		if data == nil || diff.IsBinary(data) {
			return nil
		}
		lines := diff.SplitLines(string(data))
		if lines == nil {
			lines = []string{}
		}
		return lines
	}
	if hasOld {
		l.oldLines = text(raw(oldRev, f.OldPath, maxContent))
	}
	if hasNew {
		l.newLines = text(raw(newRev, f.Path, maxContent))
	}
	return l
}

// finish colors the lines, and decodes the pictures.
func (l *loaded) finish() {
	l.oldHL = highlight.Lines(l.oldPath, l.oldLines)
	l.newHL = highlight.Lines(l.path, l.newLines)
	if l.oldData != nil {
		l.oldImage, _ = ui.DecodeBitmap(l.oldData)
	}
	if l.newData != nil {
		l.newImage, _ = ui.DecodeBitmap(l.newData)
	}
}

// apply gives the contents to their file, on the main thread.
func (l *loaded) apply() {
	f := l.file
	f.oldLines, f.newLines = l.oldLines, l.newLines
	f.oldHL, f.newHL = l.oldHL, l.newHL
	f.oldImage, f.newImage = l.oldImage, l.newImage
	f.oldSize, f.newSize = len(l.oldData), len(l.newData)
	f.loaded = true
	f.metricsDone = false
	f.spans = nil
}

// maxImage is the size of the pictures shown.
const maxImage = 32 << 20

// isImage reports whether a path is of a picture the app shows.
func isImage(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".ico":
		return true
	}
	return false
}

// loadContents reads both sides of the files, for their colors and their
// unchanged lines, and shows them as they come.
func (w *window) loadContents(gen int, oldRev, newRev string, files []*fileState) {
	defer boostGC()()
	contents, err := w.repo.NewContents()
	if err != nil {
		return
	}
	defer contents.Close()

	jobs := make(chan loaded)
	results := make(chan loaded)
	var wg sync.WaitGroup
	// A few workers: more would take the CPU, and the collector's pauses,
	// from the main thread, which draws the window.
	for range highlightWorkers() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				job.finish()
				results <- job
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, f := range files {
			if w.stale(gen) {
				return
			}
			job := w.read(contents, f.File, oldRev, newRev)
			job.file = f
			jobs <- job
		}
	}()
	go func() {
		wg.Wait()
		close(results)
	}()

	var batch []loaded
	flush := func() {
		if len(batch) == 0 {
			return
		}
		done := batch
		batch = nil
		w.update(func() {
			if gen != w.gen {
				return
			}
			for i := range done {
				done[i].apply()
			}
			w.rowsDirty = true
		})
	}
	tick := time.NewTicker(60 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case l, ok := <-results:
			if !ok {
				flush()
				return
			}
			batch = append(batch, l)
		case <-tick.C:
			flush()
		}
	}
}

// boostGC lets the heap grow further between collections while files load:
// Chroma's lexers make much garbage, and every collection has the main
// thread, which allocates as it draws, help with it. The function it
// returns ends the boost.
func boostGC() func() {
	gcBoost.Lock()
	if gcBoost.n == 0 {
		gcBoost.old = debug.SetGCPercent(400)
	}
	gcBoost.n++
	gcBoost.Unlock()
	return func() {
		gcBoost.Lock()
		gcBoost.n--
		if gcBoost.n == 0 {
			debug.SetGCPercent(gcBoost.old)
		}
		gcBoost.Unlock()
	}
}

var gcBoost struct {
	sync.Mutex
	n, old int
}

// highlightWorkers is how many files are highlighted at once.
func highlightWorkers() int { return min(max(runtime.NumCPU()/4, 1), 2) }

func (w *window) stale(gen int) bool { return w.genA.Load() != int64(gen) }

// loadUser reads the name of the git user, which comments show, off the
// main thread: running git takes a while.
func (w *window) loadUser() {
	w.background(func() {
		name := strings.TrimSpace(w.repo.ConfigValue("user.name"))
		if name == "" {
			name = strings.TrimSpace(w.repo.ConfigValue("user.email"))
		}
		w.update(func() { w.user = name })
	})
}

// loadHistory reads the commits of the History tab.
func (w *window) loadHistory() {
	limit := w.historyLimit
	w.historyGen++
	gen := w.historyGen
	w.historyLoading = true
	w.background(func() {
		commits, err := w.repo.Log(0, limit)
		if err != nil {
			commits = nil
		}
		w.update(func() {
			if gen != w.historyGen {
				return
			}
			w.history = commits
			w.historyMore = len(commits) >= limit
			w.historyLoading = false
		})
	})
}

// watch notices changes of the work tree, polling as codiff does: often
// while the window has the focus, rarely while it is in the background.
func (w *window) watch(stop chan struct{}) {
	for {
		delay := 10 * time.Second
		if w.win.IsFocused() {
			delay = 2500 * time.Millisecond
		} else if !w.win.IsVisible() || w.win.IsMinimized() {
			delay = 30 * time.Second
		}
		select {
		case <-stop:
			return
		case <-time.After(delay):
		}
		w.checkRepositoryChanges()
	}
}

// checkChanges compares the work tree with what the window shows.
func (w *window) checkChanges() {
	var src source
	var sig, head, branch string
	var historyGen int
	busy, historyBusy := true, true
	w.onMain(func() {
		src, sig, branch = w.source, w.signature, w.branch
		historyGen = w.historyGen
		if len(w.history) > 0 {
			head = w.history[0].Hash
		}
		busy, historyBusy = w.loading || w.changed || w.commitBusy || w.changeBusy, w.historyLoading
	})
	if !historyBusy {
		now, _ := w.repo.Resolve("HEAD")
		if now != head {
			w.update(func() {
				if !w.historyLoading && w.historyGen == historyGen {
					w.loadHistory()
				}
			})
		}
		if current := w.repo.Branch(); current != branch {
			remoteURL := w.repo.WebURL(current)
			w.update(func() {
				if w.branch == branch {
					w.branch, w.remoteURL = current, remoteURL
				}
			})
		}
	}
	if src.kind == sourcePull && !busy {
		w.checkPull(src)
		return
	}
	if busy || sig == "" || src.kind == sourceCommit {
		return
	}
	if now := w.repo.StatusSignature(); now != sig {
		w.update(func() {
			if w.source == src && !w.loading {
				w.changed = true
				if w.multipleRepositories() && src.kind == sourceWorkingTree && !w.commitBusy {
					w.load()
				}
			}
		})
	}
}

// refresh loads the source again, and the history, and the pull
// requests once listed.
func (w *window) refresh() {
	if w.workspace != nil {
		w.workspace.discover()
		for _, entry := range w.workspace.entries {
			if entry.view != nil {
				entry.view.refreshRepository()
			}
		}
		return
	}
	w.refreshRepository()
}

func (w *window) refreshRepository() {
	if w.commitBusy {
		return
	}
	w.load()
	w.loadHistory()
	if w.pullsLoaded {
		w.loadPulls()
	}
}

// openInEditor opens a file of the repository in the user's editor, at a
// line.
func (w *window) openInEditor(path string, line int) {
	abs := filepath.Join(w.repo.Root, filepath.FromSlash(path))
	if _, err := os.Stat(abs); err != nil {
		abs = w.repo.Root
		line = 0
	}
	go func() {
		if err := openEditor(w.settings.EditorCommand, w.repo.Root, abs, line); err != nil {
			mygo.Dialog.Error("Could not open the file", err.Error())
		}
	}()
}

// abbreviateHome writes the home directory as ~.
func abbreviateHome(p string) string {
	home, err := os.UserHomeDir()
	if err == nil && (p == home || strings.HasPrefix(p, home+string(filepath.Separator))) {
		return "~" + p[len(home):]
	}
	return p
}

// shortPath abbreviates the directories of a path to their first letter,
// as fish does: ~/P/fate.
func shortPath(p string) string {
	p = abbreviateHome(p)
	parts := strings.Split(p, "/")
	for i := 1; i < len(parts)-1; i++ {
		part := parts[i]
		if part == "" || part == "~" {
			continue
		}
		n := 1
		if strings.HasPrefix(part, ".") && len(part) > 1 {
			n = 2
		}
		r := []rune(part)
		parts[i] = string(r[:min(n, len(r))])
	}
	return strings.Join(parts, "/")
}

// errorText is the text of an error for the window.
func errorText(err error) string {
	var ge *git.Error
	if errors.As(err, &ge) && strings.TrimSpace(ge.Stderr) != "" {
		return strings.TrimSpace(ge.Stderr)
	}
	return fmt.Sprint(err)
}

// probeMainThread logs how long the main thread kept a function waiting,
// at worst, over a while: how long the window could not respond.
func probeMainThread(d time.Duration) {
	var worst, total time.Duration
	var slow, n int
	for end := time.Now().Add(d); time.Now().Before(end); n++ {
		start := time.Now()
		mygo.RunOnMain(func() {})
		wait := time.Since(start)
		total += wait
		worst = max(worst, wait)
		if wait > 16*time.Millisecond {
			slow++
		}
		time.Sleep(2 * time.Millisecond)
	}
	log.Printf("main thread: worst wait %v, average %v, %d of %d waits over 16ms", worst, total/time.Duration(max(n, 1)), slow, n)
}

// watchMainThread logs, for GODIFF_DEBUG, whenever the main thread keeps
// a function waiting over 30ms: when the window cannot respond.
func watchMainThread() {
	limit := 30 * time.Millisecond
	if ms, err := strconv.Atoi(os.Getenv("GODIFF_DEBUG_BLOCK_MS")); err == nil {
		limit = time.Duration(ms) * time.Millisecond
	}
	var gc debug.GCStats
	for {
		start := time.Now()
		mygo.RunOnMain(func() {})
		if wait := time.Since(start); wait > limit {
			n := gc.NumGC
			debug.ReadGCStats(&gc)
			pause := time.Duration(0)
			if len(gc.Pause) > 0 {
				pause = gc.Pause[0]
			}
			log.Printf("main thread blocked %v (GC cycles since last: %d, last pause %v)", wait, gc.NumGC-n, pause)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
