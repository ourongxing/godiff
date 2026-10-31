package main

import (
	"context"
	"slices"
	"strings"

	"github.com/egoist/godiff/internal/diff"
	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

// reviewChanges selects the correct pair of revisions before revealing a file.
func (w *window) reviewChanges(scope, path string) {
	w.activateRepository()
	w.tab, w.commitOpen = 0, false
	src := source{kind: sourceWorkingTree, ref: scope}
	if w.source != src {
		w.setSourceFile(src, path)
		return
	}
	for i, f := range w.files {
		if f.Path == path {
			w.revealFile(i)
			return
		}
	}
}

func changePaths(files []*diff.File) []string {
	var paths []string
	for _, f := range files {
		if f.Directory && strings.HasPrefix(f.Path, "Untracked files not shown") {
			continue // the synthetic "more untracked files" row is not a path.
		}
		paths = append(paths, f.Path)
		if f.Status == diff.Renamed && f.OldPath != f.Path {
			paths = append(paths, f.OldPath)
		}
	}
	return slices.Compact(paths)
}

func (w *window) changeIndex(staged bool, files []*diff.File) {
	paths := changePaths(files)
	if len(paths) == 0 {
		return
	}
	w.activateRepository()
	w.runChange(func(ctx context.Context) error {
		if staged {
			return w.repo.Unstage(ctx, paths)
		}
		return w.repo.Stage(ctx, paths)
	})
}

func (w *window) runChange(fn func(context.Context) error) {
	if w.changeBusy || w.commitBusy || w.loading {
		return
	}
	w.changeBusy, w.changeErr = true, ""
	w.background(func() {
		err := fn(w.ctx)
		w.update(func() {
			w.changeBusy = false
			if err != nil {
				w.changeErr = errorText(err)
			}
			w.load()
		})
	})
}

func (w *window) askDiscard(files []*diff.File) {
	if w.changeBusy || w.commitBusy || w.loading {
		return
	}
	w.discardChanges = nil
	for _, f := range files {
		if git.CanDiscard(f) {
			w.discardChanges = append(w.discardChanges, f)
		}
	}
	w.activateRepository()
}

func (w *window) discardChangesDialog(c *ui.Context) {
	if len(w.discardChanges) == 0 {
		return
	}
	files := w.discardChanges
	open := true
	message := "Unstaged edits will be lost and untracked files will be deleted. Staged changes are kept."
	if choice := ui.AlertDialog(c, &open, "Discard changes to "+plural(len(files), "file")+"?", message, "Cancel", "Discard Changes"); choice == 1 {
		w.discardChanges = nil
		w.runChange(func(ctx context.Context) error { return w.repo.Discard(ctx, files) })
	} else if !open {
		w.discardChanges = nil
	}
}
