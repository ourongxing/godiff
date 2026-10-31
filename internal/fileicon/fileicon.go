// Package fileicon gives files and folders the original, full-color SVGs
// and name associations of Material Icon Theme.
package fileicon

import (
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// Icon is the icon of a kind of file or folder.
type Icon struct {
	Token string
	once  sync.Once
	dark  *ui.SVG
	light *ui.SVG
}

// SVG returns the full-color icon for the appearance. Parsing is deferred
// until the icon is first displayed and shared by every file of its kind.
func (i *Icon) SVG(dark bool) *ui.SVG {
	i.once.Do(func() {
		i.dark = ui.MustParseSVG([]byte(symbols[i.Token]))
		i.light = i.dark
		if token := lightVariants[i.Token]; token != "" {
			i.light = ui.MustParseSVG([]byte(symbols[token]))
		}
	})
	if dark {
		return i.dark
	}
	return i.light
}

var (
	once  sync.Once
	icons map[string]*Icon
)

func load() {
	icons = make(map[string]*Icon, len(symbols))
	for token := range symbols {
		icons[token] = &Icon{Token: token}
	}
}

func forToken(token string) *Icon {
	once.Do(load)
	return icons[token]
}

// For returns the icon of a file by its path.
func For(path string) *Icon { return forToken(Token(path)) }

// ForFolder returns a folder's icon, including its expanded state.
func ForFolder(path string, expanded bool) *Icon {
	name := strings.ToLower(path[strings.LastIndexByte(path, '/')+1:])
	byName, fallback := folderNames, defaultFolder
	if expanded {
		byName, fallback = folderNamesExpanded, defaultFolderExpanded
	}
	if token := byName[name]; token != "" {
		return forToken(token)
	}
	return forToken(fallback)
}

// Token returns a file's icon name. Specific names and path suffixes take
// precedence over extensions, which are tried longest first (.d.ts before .ts).
func Token(path string) string {
	path = strings.ToLower(strings.ReplaceAll(path, `\`, "/"))
	for suffix := path; ; {
		if token, ok := byName[suffix]; ok {
			return token
		}
		i := strings.IndexByte(suffix, '/')
		if i < 0 {
			break
		}
		suffix = suffix[i+1:]
	}
	name := path[strings.LastIndexByte(path, '/')+1:]
	for i := strings.IndexByte(name, '.'); i >= 0; {
		ext := name[i+1:]
		if token, ok := byExtension[ext]; ok {
			return token
		}
		j := strings.IndexByte(ext, '.')
		if j < 0 {
			break
		}
		i += j + 1
	}
	return defaultFile
}
