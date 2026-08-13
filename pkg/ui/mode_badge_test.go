// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/awscfg"
	"github.com/uraniumdawn/skog/pkg/config"
)

// newBadgeApp builds the least application the badge needs — a selected profile in the given mode,
// a layout and one bordered page — drawn once onto a simulation screen of the given size.
//
// It restores the tview package-level state NewLayout and ApplyColors write, so a test cannot leak
// borders or colors into the next one.
func newBadgeApp(
	t *testing.T,
	mode config.Mode,
	width, height int,
) (*App, tcell.SimulationScreen) {
	t.Helper()

	styles, borders := tview.Styles, tview.Borders
	t.Cleanup(func() { tview.Styles, tview.Borders = styles, borders })

	colors, err := config.LoadColorConfig("")
	if err != nil {
		t.Fatalf("LoadColorConfig() error = %v", err)
	}

	app := &App{
		Application:    tview.NewApplication(),
		Config:         &config.Config{},
		Colors:         colors,
		CurrentFilters: make(map[string]string),
		Selected:       Selected{Profile: &awscfg.Profile{Name: "test"}},
	}
	app.Config.SetProfileMode("test", mode)

	app.ApplyColors()
	registry := NewPagesRegistry(app.Colors)
	app.Layout = NewLayout(registry, app.Colors)

	// Added straight to Pages: AddToPagesRegistry needs the cache and appends a timestamp to the
	// title, neither of which the badge cares about.
	page := tview.NewTable()
	page.SetBorder(true).SetTitle(" Profiles ").SetTitleAlign(tview.AlignLeft)
	registry.UI.Pages.AddAndSwitchToPage(Profiles, page, true)

	screen := tcell.NewSimulationScreen("UTF-8")
	t.Cleanup(screen.Fini)

	// SetScreen initialises the screen, which resets it to its default size — so the size this
	// test wants has to be set after, not before.
	app.SetScreen(screen)
	screen.SetSize(width, height)

	app.SetAfterDrawFunc(app.drawModeBadge)
	app.SetRoot(app.Layout.Content, true)
	app.ForceDraw()

	return app, screen
}

// screenRow returns row y of the screen as text. A cell nothing was drawn into holds no runes at
// all, which reads as a blank.
func screenRow(t *testing.T, screen tcell.SimulationScreen, y int) string {
	t.Helper()

	cells, width, height := screen.GetContents()
	if y < 0 || y >= height {
		t.Fatalf("row %d is outside a screen of %d rows", y, height)
	}

	var row strings.Builder
	for x := range width {
		cell := cells[y*width+x]
		if len(cell.Runes) == 0 {
			row.WriteRune(' ')
			continue
		}
		row.WriteRune(cell.Runes[0])
	}
	return row.String()
}

// screenFg returns the foreground color of one cell.
func screenFg(t *testing.T, screen tcell.SimulationScreen, x, y int) tcell.Color {
	t.Helper()

	cells, width, _ := screen.GetContents()
	fg, _, _ := cells[y*width+x].Style.Decompose()
	return fg
}

// The badge belongs at the right end of the content area's top border line, and the page title
// keeps the left end of the same line.
func TestModeBadgeIsRightAlignedOnTheContentBorder(t *testing.T) {
	_, screen := newBadgeApp(t, config.Yolo, 80, 24)

	row := screenRow(t, screen, headerHeight)
	want := " yolo " + string(tview.Borders.TopRight)
	if !strings.HasSuffix(row, want) {
		t.Errorf("content border row = %q, want it to end with %q", row, want)
	}
	if !strings.Contains(row, " Profiles ") {
		t.Errorf("content border row = %q, want the page title kept", row)
	}
}

func TestModeBadgeTextPerMode(t *testing.T) {
	for _, mode := range []config.Mode{config.ReadOnly, config.Regular, config.Yolo} {
		t.Run(string(mode), func(t *testing.T) {
			_, screen := newBadgeApp(t, mode, 80, 24)

			row := screenRow(t, screen, headerHeight)
			want := " " + string(mode) + " " + string(tview.Borders.TopRight)
			if !strings.HasSuffix(row, want) {
				t.Errorf("content border row = %q, want it to end with %q", row, want)
			}
		})
	}
}

// Only the badge text carries the mode's color; the border it sits in keeps the style file's.
func TestModeBadgeColorsOnlyItself(t *testing.T) {
	app, screen := newBadgeApp(t, config.Yolo, 80, 24)

	_, _, width, _ := app.Layout.PagesRegistry.UI.Pages.GetRect()
	badge := tview.TaggedStringWidth(modeBadgeText(config.Yolo))
	wantBadge := tcell.GetColor(app.Colors.Skog.Mode.Yolo)

	for x := width - modeBadgeMargin - badge; x < width-modeBadgeMargin; x++ {
		if got := screenFg(t, screen, x, headerHeight); got != wantBadge {
			t.Fatalf("badge cell %d color = %v, want %v", x, got, wantBadge)
		}
	}

	wantBorder := tcell.GetColor(app.Colors.Skog.Border)
	if got := screenFg(t, screen, width-1, headerHeight); got != wantBorder {
		t.Errorf("corner color = %v, want the border's %v", got, wantBorder)
	}
}

// A terminal too narrow for both keeps the title: a half-eaten badge tells the user nothing.
func TestModeBadgeHiddenOnNarrowTerminal(t *testing.T) {
	_, screen := newBadgeApp(t, config.Yolo, 20, 24)

	row := screenRow(t, screen, headerHeight)
	// Guards the test itself: the screen must really be the narrow one that was asked for.
	if got := len([]rune(row)); got != 20 {
		t.Fatalf("screen width = %d, want 20", got)
	}
	if strings.Contains(row, "yolo") {
		t.Errorf("content border row = %q, want no badge on a narrow terminal", row)
	}
}

// The badge is positioned from the pages' own rect, so it follows the content area when the inline
// search pushes it down.
func TestModeBadgeFollowsInlineSearch(t *testing.T) {
	app, screen := newBadgeApp(t, config.Yolo, 80, 24)

	app.Layout.Search[Profiles] = NewInlineSearch(app.Colors)
	app.Layout.ShowInlineSearch(Profiles)
	app.ForceDraw()

	if row := screenRow(t, screen, headerHeight); strings.Contains(row, "yolo") {
		t.Errorf("row %d = %q, want the badge gone from the old position", headerHeight, row)
	}
	moved := headerHeight + searchHeight
	if row := screenRow(t, screen, moved); !strings.Contains(row, " yolo ") {
		t.Errorf("row %d = %q, want the badge to have moved there", moved, row)
	}
}

// The badge paints last, so a modal on top of a page cannot cover it.
func TestModeBadgeSurvivesAModal(t *testing.T) {
	app, screen := newBadgeApp(t, config.Yolo, 80, 24)

	pages := app.Layout.PagesRegistry.UI.Pages
	pages.AddPage(OpenedPages, app.Layout.PagesRegistry.UI.Main, true, true)
	pages.SendToFront(OpenedPages)
	app.ForceDraw()

	if row := screenRow(t, screen, headerHeight); !strings.Contains(row, " yolo ") {
		t.Errorf("content border row = %q, want the badge on top of the modal", row)
	}
}

// tview.Print reads square brackets as style tags, so a bracketed badge would print as nothing.
func TestModeBadgeTextCarriesNoStyleTags(t *testing.T) {
	for _, mode := range []config.Mode{config.ReadOnly, config.Regular, config.Yolo} {
		if text := modeBadgeText(mode); strings.ContainsAny(text, "[]") {
			t.Errorf("modeBadgeText(%q) = %q, want no square brackets", mode, text)
		}
	}
}
