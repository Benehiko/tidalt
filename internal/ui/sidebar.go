package ui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Section is the selected left-sidebar item — the primary navigation axis that
// replaces the old flat State/tab enum.
type Section int

const (
	SecNowPlaying Section = iota
	SecQueue
	SecPlaylists
	SecFavSongs
	SecFavArtists
	SecFavAlbums
	SecHistory // Recently Played
	SecMixes   // Daily Mixes
	SecSearch
	SecSettings // theme picker
)

// sidebarEntry is one navigable row in the sidebar. A group header (label only)
// is represented by an entry with section == -1.
type sidebarEntry struct {
	section Section
	group   string // non-empty => this is a group header, not a selectable item
	icon    string
	label   string
}

// sidebarEntries is the fixed sidebar layout: three groups (NOW / LIBRARY /
// TIDAL) with their items, plus a trailing Settings item.
var sidebarEntries = []sidebarEntry{
	{group: "NOW"},
	{section: SecNowPlaying, icon: "♪", label: "Now Playing"},
	{section: SecQueue, icon: "≣", label: "Queue"},
	{group: "LIBRARY"},
	{section: SecPlaylists, icon: "≡", label: "Playlists"},
	{section: SecFavSongs, icon: "♥", label: "Songs"},
	{section: SecFavArtists, icon: "◎", label: "Artists"},
	{section: SecFavAlbums, icon: "⊞", label: "Albums"},
	{section: SecHistory, icon: "↺", label: "Recently Played"},
	{group: "TIDAL"},
	{section: SecMixes, icon: "✦", label: "Daily Mixes"},
	{section: SecSearch, icon: "/", label: "Search"},
	{group: "SETTINGS"},
	{section: SecSettings, icon: "◐", label: "Themes"},
}

// navSections is the ordered list of selectable sections (group headers
// removed), used for j/k navigation in the sidebar.
var navSections = func() []Section {
	var out []Section
	for _, e := range sidebarEntries {
		if e.group == "" {
			out = append(out, e.section)
		}
	}
	return out
}()

// sectionCount returns the badge count for a section, or -1 to omit it.
func (m *Model) sectionCount(sec Section) int {
	switch sec {
	case SecQueue:
		return len(m.tracks)
	case SecPlaylists:
		return len(m.playlists)
	case SecFavSongs:
		return len(m.favorites)
	case SecFavArtists:
		return len(m.favArtists)
	case SecFavAlbums:
		return len(m.favAlbums)
	default:
		return -1
	}
}

// renderSidebar draws the left navigation column to exactly h lines, width w.
// The header is the scaled logo (plus a CLIENT badge in client mode); the active
// section's label is cyan, and the row at sidebarCursor gets the selected band
// when the sidebar (not the main pane) holds focus.
func (m *Model) renderSidebar(t Theme, w, h int) string {
	var lines []string

	// Header: compact logo.
	logo := renderLogo(m.logoFrame, m.barFrame, m.barHeights, t, 0, m.isPlaying)
	lines = append(lines, strings.Split(strings.TrimRight(logo, "\n"), "\n")...)
	if m.clientMode {
		lines = append(lines, t.SideItemActive.Render(" ⇄ CLIENT"))
	}
	lines = append(lines, "")

	cursorSec := navSections[m.sidebarCursor]
	for _, e := range sidebarEntries {
		if e.group != "" {
			lines = append(lines, t.SideGroup.Render(" "+e.group))
			continue
		}
		// The row under the sidebar cursor (only when the sidebar holds focus)
		// gets the solid selection band; the active section's label stays cyan.
		cursorOn := !m.focusMain && e.section == cursorSec
		active := e.section == m.section
		lines = append(lines, m.sidebarRow(t, e, w, cursorOn, active))
	}

	return padLines(lines, w, h)
}

// sidebarRow renders a single selectable sidebar item: icon + label, with an
// optional right-aligned count. cursorOn draws a solid selection band; active
// (without cursor) tints the icon and label cyan.
func (m *Model) sidebarRow(t Theme, e sidebarEntry, w int, cursorOn, active bool) string {
	label := e.label
	content := fmt.Sprintf(" %s %s", e.icon, label)
	if count := m.sectionCount(e.section); count >= 0 {
		cstr := strconv.Itoa(count)
		pad := max(w-lipgloss.Width(content)-len(cstr)-1, 1)
		content += strings.Repeat(" ", pad) + cstr
	}

	switch {
	case cursorOn:
		return t.SideItemSel.Width(w).Render(content)
	case active:
		icon := t.SideIconSel.Render(e.icon)
		lbl := t.SideItemActive.Render(label)
		row := " " + icon + " " + lbl
		if count := m.sectionCount(e.section); count >= 0 {
			cstr := t.SideCount.Render(strconv.Itoa(count))
			pad := max(w-lipgloss.Width(row)-lipgloss.Width(cstr)-1, 1)
			row += strings.Repeat(" ", pad) + cstr
		}
		return row
	default:
		return t.SideItem.Render(content)
	}
}

// navIndexOf returns the index of a section within navSections.
func navIndexOf(sec Section) int {
	for i, s := range navSections {
		if s == sec {
			return i
		}
	}
	return 0
}

// sectionTitle returns the panel title for a section.
func sectionTitle(sec Section) string {
	switch sec {
	case SecNowPlaying:
		return "NOW PLAYING"
	case SecQueue:
		return "QUEUE"
	case SecPlaylists:
		return "PLAYLISTS"
	case SecFavSongs:
		return "SONGS"
	case SecFavArtists:
		return "ARTISTS"
	case SecFavAlbums:
		return "ALBUMS"
	case SecHistory:
		return "RECENTLY PLAYED"
	case SecMixes:
		return "DAILY MIXES"
	case SecSearch:
		return "SEARCH"
	case SecSettings:
		return "THEMES"
	default:
		return ""
	}
}

// padLines pads/truncates a slice of rendered lines to exactly h rows and pads
// each to width w (measuring display width, not bytes).
func padLines(lines []string, w, h int) string {
	out := make([]string, 0, h)
	for i := range h {
		ln := ""
		if i < len(lines) {
			ln = lines[i]
		}
		lw := lipgloss.Width(ln)
		if lw < w {
			ln += strings.Repeat(" ", w-lw)
		}
		out = append(out, ln)
	}
	return strings.Join(out, "\n")
}
