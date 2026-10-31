package main

import (
	"github.com/egoist/godiff/internal/highlight"
	"github.com/egoist/mygo/ui"
)

// palette is the colors of the review: Licht in light mode and VS Code's
// Dark Modern in dark mode.
type palette struct {
	appBg      ui.Color // behind the cards
	code       ui.Color // the code's own color
	lineNumber ui.Color
	codeBg     ui.Color // the background of the cards
	addBg      ui.Color
	addGutter  ui.Color
	addWord    ui.Color
	addBar     ui.Color
	addText    ui.Color // +N counts
	delBg      ui.Color
	delGutter  ui.Color
	delWord    ui.Color
	delBar     ui.Color
	delText    ui.Color
	emptySide  ui.Color // the side of a split row without a line
	gapBg      ui.Color // "N unmodified lines"
	gapText    ui.Color
	cardBorder ui.Color
	headerBg   ui.Color
	pill       ui.Color // the background of counts and badges
	hover      ui.Color // what is under the pointer
	match      ui.Color // a match of the find bar
	matchNow   ui.Color // the current match
	selected   ui.Color // lines chosen with j and k
	viewed     ui.Color
	ref        ui.Color // commit hashes and generated files
	muted      ui.Color
	syntax     [highlight.NumClasses]ui.Color
}

var (
	lightPalette = palette{
		appBg:      ui.Hex("#f8f8f6"),
		code:       ui.Hex("#111111"),
		lineNumber: ui.Hex("#9a9a9a"),
		codeBg:     ui.Hex("#ffffff"),
		addBg:      ui.Hex("#e2f7ea"),
		addGutter:  ui.Hex("#d3f3df"),
		addWord:    ui.RGBA(13, 190, 78, 0.20),
		addBar:     ui.Hex("#0dbe4e"),
		addText:    ui.RGB(31, 122, 68),
		delBg:      ui.Hex("#ffe6e8"),
		delGutter:  ui.Hex("#ffd9dc"),
		delWord:    ui.RGBA(255, 46, 63, 0.17),
		delBar:     ui.Hex("#ff2e3f"),
		delText:    ui.RGB(184, 49, 47),
		emptySide:  ui.Hex("#fafafa"),
		gapBg:      ui.Hex("#f4f4f4"),
		gapText:    ui.Hex("#8a8a8a"),
		cardBorder: ui.RGBA(17, 17, 17, 0.10),
		headerBg:   ui.Hex("#ffffff"),
		pill:       ui.RGBA(127, 127, 127, 0.11),
		hover:      ui.RGBA(127, 127, 127, 0.11),
		match:      ui.RGBA(255, 216, 92, 0.65),
		matchNow:   ui.RGBA(255, 176, 46, 0.96),
		selected:   ui.RGBA(61, 135, 245, 0.13),
		viewed:     ui.RGB(31, 122, 68),
		ref:        ui.Hex("#c56e0e"),
		muted:      ui.RGBA(17, 17, 17, 0.48),
	}
	darkPalette = palette{
		appBg:      ui.Hex("#1f1f1f"),
		code:       ui.Hex("#cccccc"),
		lineNumber: ui.Hex("#6e7681"),
		codeBg:     ui.Hex("#1f1f1f"),
		addBg:      ui.RGBA(155, 185, 85, 0.20).Over(ui.Hex("#1f1f1f")),
		addGutter:  ui.RGBA(155, 185, 85, 0.20).Over(ui.Hex("#1f1f1f")),
		addWord:    ui.RGBA(156, 204, 44, 0.20),
		addBar:     ui.Hex("#2ea043"),
		addText:    ui.Hex("#73c991"),
		delBg:      ui.RGBA(255, 0, 0, 0.20).Over(ui.Hex("#1f1f1f")),
		delGutter:  ui.RGBA(255, 0, 0, 0.20).Over(ui.Hex("#1f1f1f")),
		delWord:    ui.RGBA(255, 0, 0, 0.20),
		delBar:     ui.Hex("#f85149"),
		delText:    ui.Hex("#f85149"),
		emptySide:  ui.Hex("#181818"),
		gapBg:      ui.Hex("#181818"),
		gapText:    ui.Hex("#9d9d9d"),
		cardBorder: ui.Hex("#2b2b2b"),
		headerBg:   ui.Hex("#181818"),
		pill:       ui.Hex("#313131"),
		hover:      ui.Hex("#2b2b2b"),
		match:      ui.RGBA(187, 128, 9, 0.40),
		matchNow:   ui.Hex("#9e6a03"),
		selected:   ui.RGBA(38, 79, 120, 0.50),
		viewed:     ui.Hex("#73c991"),
		ref:        ui.Hex("#4daafc"),
		muted:      ui.Hex("#9d9d9d"),
	}
)

func init() {
	// Licht is codiff's light theme. Dark Modern inherits Dark+'s tokens:
	// https://github.com/microsoft/vscode/tree/main/extensions/theme-defaults/themes
	light := map[highlight.Class]string{
		highlight.Comment: "#919191", highlight.Preproc: "#adadad", highlight.Keyword: "#352de3",
		highlight.Type: "#c56e0e", highlight.LangConst: "#626fc9", highlight.Function: "#284181",
		highlight.ClassName: "#bb28c7", highlight.Exception: "#f93232", highlight.Number: "#dd3c2f",
		highlight.String: "#00a33f", highlight.Escape: "#00a33f", highlight.Regexp: "#699d36",
		highlight.Tag: "#0072c8", highlight.Attribute: "#0072c8", highlight.Property: "#444444",
		highlight.Heading: "#111111", highlight.Inserted: "#1a7f37", highlight.Deleted: "#c4232e",
	}
	dark := map[highlight.Class]string{
		highlight.Comment: "#6a9955", highlight.Preproc: "#569cd6", highlight.Keyword: "#569cd6",
		highlight.Type: "#4ec9b0", highlight.LangConst: "#569cd6", highlight.Function: "#dcdcaa",
		highlight.ClassName: "#4ec9b0", highlight.Exception: "#4ec9b0", highlight.Number: "#b5cea8",
		highlight.String: "#ce9178", highlight.Escape: "#d7ba7d", highlight.Regexp: "#d16969",
		highlight.Tag: "#569cd6", highlight.Attribute: "#9cdcfe", highlight.Property: "#9cdcfe",
		highlight.Heading: "#569cd6", highlight.Inserted: "#b5cea8", highlight.Deleted: "#ce9178",
	}
	lightPalette.syntax[highlight.Plain] = lightPalette.code
	darkPalette.syntax[highlight.Plain] = darkPalette.code
	for k, v := range light {
		lightPalette.syntax[k] = ui.Hex(v)
	}
	for k, v := range dark {
		darkPalette.syntax[k] = ui.Hex(v)
	}
}

// applyReviewTheme gives controls the same Dark Modern colors as the diff,
// retaining the system's font and metrics from the frame's theme.
func applyReviewTheme(c *ui.Context) {
	if !c.Theme().Dark {
		return
	}
	t := *c.Theme()
	t.Background = darkPalette.appBg
	t.Surface = ui.Hex("#313131")
	t.SurfaceHover = ui.Hex("#3c3c3c")
	t.SurfacePressed = ui.Hex("#2b2b2b")
	t.Border = ui.Hex("#3c3c3c")
	t.Text = darkPalette.code
	t.TextMuted = darkPalette.muted
	t.Accent = ui.Hex("#0078d4")
	t.AccentHover = ui.Hex("#026ec1")
	t.AccentPressed = ui.Hex("#005a9e")
	t.AccentText = ui.Hex("#ffffff")
	t.Danger = ui.Hex("#f85149")
	t.Warning = ui.Hex("#cca700")
	t.Success = darkPalette.addText
	t.Selection = ui.Hex("#264f78")
	t.Focus = t.Accent
	t.Inverse = ui.Hex("#202020")
	t.InverseText = t.Text
	t.Scrollbar = ui.RGBA(121, 121, 121, 0.40)
	c.SetTheme(&t)
}

func paletteFor(t *ui.Theme) *palette {
	if t.Dark {
		return &darkPalette
	}
	return &lightPalette
}
