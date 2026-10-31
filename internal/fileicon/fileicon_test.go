package fileicon

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestToken(t *testing.T) {
	for path, want := range map[string]string{
		"main.go":                          "go",
		"web/src/App.tsx":                  "react_ts",
		"web/src/app.ts":                   "typescript",
		"types/global.d.ts":                "typescript-def",
		"tests/app.test.ts":                "test-ts",
		"lib/index.mjs":                    "javascript",
		"Dockerfile":                       "docker",
		"deploy/docker-compose.yml":        "docker",
		".gitignore":                       "git",
		"README.md":                        "readme",
		"pnpm-lock.yaml":                   "pnpm",
		"bun.lock":                         "bun",
		"vite.config.ts":                   "vite",
		"styles/site.scss":                 "sass",
		"logo.PNG":                         "image",
		"LICENSE":                          "license",
		"Makefile.unknown-kind":            "file",
		"project/.config/stylelintrc.json": "stylelint",
		`web\src\App.tsx`:                  "react_ts",
	} {
		if got := Token(path); got != want {
			t.Errorf("%s: %s, want %s", path, got, want)
		}
	}
}

func TestIcons(t *testing.T) {
	for token, source := range symbols {
		svg, err := ui.ParseSVG([]byte(source))
		if err != nil {
			t.Errorf("%s: %v", token, err)
			continue
		}
		if w, h := svg.Size(); w <= 0 || h <= 0 {
			t.Errorf("%s: invalid size %v x %v", token, w, h)
		}
	}
	for _, associations := range []map[string]string{byName, byExtension, folderNames, folderNamesExpanded, lightVariants} {
		for name, token := range associations {
			if symbols[token] == "" {
				t.Errorf("%s: no SVG for %s", name, token)
			}
		}
	}
	if i := For("main.go"); i.SVG(true) != i.SVG(false) || i != For("other.go") {
		t.Error("Go files should share their icon in both appearances")
	}
	if i := For("next.config.ts"); i.SVG(true) == i.SVG(false) {
		t.Error("Next.js should have a dedicated light-mode icon")
	}
}

func TestFolderIcons(t *testing.T) {
	for _, tc := range []struct {
		path     string
		expanded bool
		want     string
	}{
		{"project/src", false, "folder-src"},
		{"project/src", true, "folder-src-open"},
		{"docs", true, "folder-docs-open"},
		{"unrecognized-folder", false, "folder"},
		{"unrecognized-folder", true, "folder-open"},
	} {
		if got := ForFolder(tc.path, tc.expanded); got.Token != tc.want || got.SVG(true) == nil {
			t.Errorf("%s (expanded %v): %+v, want %s", tc.path, tc.expanded, got, tc.want)
		}
	}
}
