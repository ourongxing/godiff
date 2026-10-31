package git

import (
	"bytes"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Subrepository is a registered submodule or an untracked nested repository.
// Path is relative to the repository discovery started from. Uninitialized
// submodules remain listed, without being opened as their parent repository.
type Subrepository struct {
	Path        string
	Root        string
	Initialized bool
}

// Subrepositories finds local child repositories recursively. Git supplies
// the candidates, so ignored dependency and build directories are not scanned.
// Discovery never initializes submodules or changes their work trees.
func (r *Repo) Subrepositories() ([]Subrepository, error) {
	var found []Subrepository
	seen := map[string]bool{r.Root: true}
	var visit func(*Repo) error
	visit = func(parent *Repo) error {
		tracked, err := parent.Git("ls-files", "--stage", "-z")
		if err != nil {
			return err
		}
		candidates := map[string]bool{}
		for _, record := range bytes.Split(tracked, []byte{0}) {
			header, path, ok := bytes.Cut(record, []byte{'\t'})
			if ok && bytes.HasPrefix(header, []byte("160000 ")) {
				candidates[string(path)] = true
			}
		}
		untracked, err := parent.Git("ls-files", "--others", "--exclude-standard", "-z")
		if err != nil {
			return err
		}
		for _, path := range bytes.Split(untracked, []byte{0}) {
			if bytes.HasSuffix(path, []byte{'/'}) {
				candidates[strings.TrimSuffix(string(path), "/")] = false
			}
		}
		for _, path := range slices.Sorted(maps.Keys(candidates)) {
			root := filepath.Join(parent.Root, filepath.FromSlash(path))
			rel, err := filepath.Rel(r.Root, root)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
			child := Subrepository{Path: filepath.ToSlash(rel), Root: root}
			if fi, err := os.Lstat(root); err == nil && fi.IsDir() {
				if canonical, err := filepath.EvalSymlinks(root); err == nil {
					if repo, err := Open(root); err == nil && repo.Root == canonical {
						child.Root, child.Initialized = repo.Root, true
					}
				}
			}
			if seen[child.Root] || (!child.Initialized && !candidates[path]) {
				continue
			}
			seen[child.Root] = true
			found = append(found, child)
			if child.Initialized {
				if err := visit(&Repo{Root: child.Root}); err != nil {
					return err
				}
			}
		}
		return nil
	}
	err := visit(r)
	return found, err
}
