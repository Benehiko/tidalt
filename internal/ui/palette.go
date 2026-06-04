package ui

import (
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/lipgloss"
)

// progressWithTheme builds a progress bar using the theme's gradient at width w.
func progressWithTheme(t Theme, w int) progress.Model {
	return progress.New(
		progress.WithGradient(t.ProgGradFrom, t.ProgGradTo),
		progress.WithWidth(w),
	)
}

// Palette is the full set of color tokens that define a TIDALT color scheme.
// Field types are lipgloss.TerminalColor (not lipgloss.Color) so the "Auto"
// scheme can hold lipgloss.AdaptiveColor values that flip with the terminal's
// background; the fixed built-in schemes use lipgloss.Color string literals.
//
// The token names mirror the design handoff's terminal.css custom properties
// 1:1 (e.g. CyanSoft = --cyan-soft, ProgA = --prog-a, Grad1 = --grad-1).
type Palette struct {
	// Surfaces.
	Bg      lipgloss.TerminalColor // terminal background
	Bg2     lipgloss.TerminalColor // slightly raised panel fill
	BgSel   lipgloss.TerminalColor // selected row band
	BgModal lipgloss.TerminalColor // command palette / sheet fill

	// Text.
	Fg      lipgloss.TerminalColor // primary mono text
	FgDim   lipgloss.TerminalColor // secondary / metadata
	FgFaint lipgloss.TerminalColor // borders-as-text, separators

	// Accents.
	Cyan     lipgloss.TerminalColor // now-playing, primary highlight / focus
	CyanSoft lipgloss.TerminalColor // active tab / selected command item fill
	Teal     lipgloss.TerminalColor
	Green    lipgloss.TerminalColor
	Purple   lipgloss.TerminalColor
	Indigo   lipgloss.TerminalColor
	Amber    lipgloss.TerminalColor
	Rose     lipgloss.TerminalColor

	// Borders.
	Border       lipgloss.TerminalColor
	BorderSoft   lipgloss.TerminalColor
	BorderAccent lipgloss.TerminalColor

	// Progress gradient endpoints.
	ProgA lipgloss.TerminalColor
	ProgB lipgloss.TerminalColor

	// Logo wordmark + EQ gradient stops.
	Grad1 lipgloss.TerminalColor
	Grad2 lipgloss.TerminalColor
	Grad3 lipgloss.TerminalColor
	Grad4 lipgloss.TerminalColor

	// OnAccent is the text color drawn on top of a Cyan/CyanSoft selection band
	// (the dark "#0f2733" in the handoff). Most schemes can reuse their Bg here.
	OnAccent lipgloss.TerminalColor
}

// Theme holds lipgloss.Style values pre-derived from a Palette so View() does no
// per-frame style construction. Build one with (Palette).Theme().
type Theme struct {
	P Palette

	// Panels (lipgloss RoundedBorder look).
	Panel         lipgloss.Style // unfocused panel border
	PanelFocus    lipgloss.Style // focused panel border (cyan)
	PanelTitle    lipgloss.Style // title spliced onto the top border (dim)
	PanelTitleHot lipgloss.Style // focused panel title (cyan)

	// List rows.
	Row        lipgloss.Style // base row text
	RowSel     lipgloss.Style // selected row band
	RowPlaying lipgloss.Style // playing row title (cyan)
	RowDim     lipgloss.Style // artist / metadata
	RowFaint   lipgloss.Style // index, badge, dash
	Fav        lipgloss.Style // favorite heart (rose)

	// Sidebar.
	SideGroup      lipgloss.Style // group label (NOW / LIBRARY / TIDAL)
	SideItem       lipgloss.Style
	SideItemSel    lipgloss.Style // selected item band
	SideItemActive lipgloss.Style // active section label (cyan)
	SideIcon       lipgloss.Style
	SideIconSel    lipgloss.Style // cyan icon on selected item
	SideCount      lipgloss.Style // trailing count (faint)

	// Command palette / action sheet items.
	CmdItem    lipgloss.Style
	CmdItemSel lipgloss.Style // cyan background, dark fg
	CmdGroup   lipgloss.Style
	CmdHint    lipgloss.Style

	// Key bar.
	KeyBarKey   lipgloss.Style // cyan
	KeyBarSep   lipgloss.Style // faint │
	KeyBarLabel lipgloss.Style // dim

	// Status / chrome.
	Header lipgloss.Style // "Playing: …" (cyan bold)
	Toast  lipgloss.Style // green save confirmation
	Err    lipgloss.Style // error banner
	Amber  lipgloss.Style // "edited / unsaved — save" hint
	GreenT lipgloss.Style // "synced" hint

	// Progress gradient endpoints (hex strings for progress.WithGradient).
	ProgGradFrom string
	ProgGradTo   string
}

// hexOf returns the hex string of a TerminalColor when it is a plain
// lipgloss.Color, else "". Used to feed progress.WithGradient (which only takes
// hex strings); Auto's adaptive progress falls back to a sensible default.
func hexOf(c lipgloss.TerminalColor) string {
	if col, ok := c.(lipgloss.Color); ok {
		return string(col)
	}
	return ""
}

// Theme derives a full set of lipgloss styles from the palette.
func (p Palette) Theme() Theme {
	rounded := lipgloss.RoundedBorder()

	from, to := hexOf(p.ProgA), hexOf(p.ProgB)
	if from == "" || to == "" {
		// Auto / adaptive palette: progress.WithGradient needs concrete hex.
		from, to = "#5b6ee1", "#a05be1"
	}

	return Theme{
		P: p,

		Panel:         lipgloss.NewStyle().Border(rounded).BorderForeground(p.Border),
		PanelFocus:    lipgloss.NewStyle().Border(rounded).BorderForeground(p.Cyan),
		PanelTitle:    lipgloss.NewStyle().Foreground(p.FgDim),
		PanelTitleHot: lipgloss.NewStyle().Foreground(p.Cyan),

		Row:        lipgloss.NewStyle().Foreground(p.Fg),
		RowSel:     lipgloss.NewStyle().Foreground(p.Fg).Background(p.BgSel),
		RowPlaying: lipgloss.NewStyle().Foreground(p.Cyan),
		RowDim:     lipgloss.NewStyle().Foreground(p.FgDim),
		RowFaint:   lipgloss.NewStyle().Foreground(p.FgFaint),
		Fav:        lipgloss.NewStyle().Foreground(p.Rose),

		SideGroup:      lipgloss.NewStyle().Foreground(p.FgFaint),
		SideItem:       lipgloss.NewStyle().Foreground(p.Fg),
		SideItemSel:    lipgloss.NewStyle().Foreground(p.Fg).Background(p.BgSel),
		SideItemActive: lipgloss.NewStyle().Foreground(p.Cyan),
		SideIcon:       lipgloss.NewStyle().Foreground(p.FgDim),
		SideIconSel:    lipgloss.NewStyle().Foreground(p.Cyan),
		SideCount:      lipgloss.NewStyle().Foreground(p.FgFaint),

		CmdItem:    lipgloss.NewStyle().Foreground(p.Fg),
		CmdItemSel: lipgloss.NewStyle().Foreground(p.OnAccent).Background(p.Cyan),
		CmdGroup:   lipgloss.NewStyle().Foreground(p.FgFaint),
		CmdHint:    lipgloss.NewStyle().Foreground(p.FgFaint),

		KeyBarKey:   lipgloss.NewStyle().Foreground(p.Cyan),
		KeyBarSep:   lipgloss.NewStyle().Foreground(p.FgFaint),
		KeyBarLabel: lipgloss.NewStyle().Foreground(p.FgDim),

		Header: lipgloss.NewStyle().Foreground(p.Cyan).Bold(true),
		Toast:  lipgloss.NewStyle().Foreground(p.Green).Border(rounded).BorderForeground(p.Green),
		Err:    lipgloss.NewStyle().Foreground(p.Rose).Bold(true),
		Amber:  lipgloss.NewStyle().Foreground(p.Amber),
		GreenT: lipgloss.NewStyle().Foreground(p.Green),

		ProgGradFrom: from,
		ProgGradTo:   to,
	}
}

// autoPalette builds a palette that follows the terminal's own colors: the
// surfaces and text use AdaptiveColor (flipping on light/dark backgrounds) and
// the accents map onto the terminal's ANSI palette so the UI inherits the
// user's configured theme.
func autoPalette() Palette {
	adaptive := func(light, dark string) lipgloss.TerminalColor {
		return lipgloss.AdaptiveColor{Light: light, Dark: dark}
	}
	ansi := func(n string) lipgloss.TerminalColor { return lipgloss.Color(n) }

	return Palette{
		Bg:      adaptive("#fafafa", "#1a1a1a"),
		Bg2:     adaptive("#f0f0f0", "#242424"),
		BgSel:   adaptive("#e0e0e0", "#323232"),
		BgModal: adaptive("#ffffff", "#101010"),

		Fg:      adaptive("#1a1a1a", "#d0d0d0"),
		FgDim:   adaptive("#6a6a6a", "#909090"),
		FgFaint: adaptive("#9a9a9a", "#5a5a5a"),

		Cyan:     ansi("6"),  // terminal cyan
		CyanSoft: ansi("14"), // bright cyan
		Teal:     ansi("6"),
		Green:    ansi("2"),
		Purple:   ansi("5"),
		Indigo:   ansi("4"),
		Amber:    ansi("3"),
		Rose:     ansi("1"),

		Border:       adaptive("#cccccc", "#3a3a3a"),
		BorderSoft:   adaptive("#dddddd", "#2a2a2a"),
		BorderAccent: ansi("6"),

		ProgA: ansi("4"),
		ProgB: ansi("5"),

		Grad1: ansi("6"),
		Grad2: ansi("14"),
		Grad3: ansi("4"),
		Grad4: ansi("5"),

		OnAccent: adaptive("#ffffff", "#000000"),
	}
}
