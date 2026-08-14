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

	screen := tcell.NewSimulationScreen("UTF-8")
	t.Cleanup(screen.Fini)

	// SetScreen initialises the screen, which resets it to its default size — so the size this
	// test wants has to be set after, not before.
	app.SetScreen(screen)
	screen.SetSize(width, height)

	app.SetAfterDrawFunc(app.drawModeBadge)
	app.SetRoot(app.Layout.Content, true)
	showPage(app, Profiles, " Profiles ")

	return app, screen
}

// showPage adds a bordered page with the given title and brings it to the front.
//
// It goes straight to Pages: AddToPagesRegistry needs the cache and appends a timestamp to the
// title, neither of which the badge cares about. The title alignment is left at tview's default —
// centred — because that is what skog's pages use, and it is what keeps the title clear of the
// badge now that the badge is painted at the left.
func showPage(app *App, name, title string) {
	page := tview.NewTable()
	page.SetBorder(true).SetTitle(title)
	app.Layout.PagesRegistry.UI.Pages.AddAndSwitchToPage(name, page, true)
	app.ForceDraw()
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

// The mode and the title are one label: the badge is painted immediately to the left of the title,
// not stranded at the end of the line.
func TestModeBadgeSitsBesideTheTitle(t *testing.T) {
	_, screen := newBadgeApp(t, config.Yolo, 80, 24)

	row := screenRow(t, screen, headerHeight)
	if want := " [yolo] Profiles "; !strings.Contains(row, want) {
		t.Errorf("content border row = %q, want it to contain %q", row, want)
	}
	// Beside the title, not in the corner.
	if strings.HasPrefix(row, string(tview.Borders.TopLeft)+" [yolo]") {
		t.Errorf("content border row = %q, want the badge away from the corner", row)
	}
}

func TestModeBadgeTextPerMode(t *testing.T) {
	for _, mode := range []config.Mode{config.ReadOnly, config.Regular, config.Yolo} {
		t.Run(string(mode), func(t *testing.T) {
			_, screen := newBadgeApp(t, mode, 80, 24)

			row := screenRow(t, screen, headerHeight)
			want := " [" + string(mode) + "] Profiles "
			if !strings.Contains(row, want) {
				t.Errorf("content border row = %q, want it to contain %q", row, want)
			}
		})
	}
}

// A page with no title has nothing to sit beside; the mode is still worth showing.
func TestModeBadgeOnAnUntitledPage(t *testing.T) {
	app, screen := newBadgeApp(t, config.Yolo, 80, 24)
	showPage(app, "untitled", "")

	row := screenRow(t, screen, headerHeight)
	if want := string(tview.Borders.TopLeft) + " [yolo]"; !strings.HasPrefix(row, want) {
		t.Errorf("content border row = %q, want it to start with %q", row, want)
	}
}

// A wide title on a narrow terminal reaches the badge's cells. The badge paints last, so it would
// eat the beginning of the title — the resource name — and leave the timestamp. The title wins
// that fight.
func TestModeBadgeStaysOffWhenItWouldEatTheTitle(t *testing.T) {
	app, screen := newBadgeApp(t, config.Yolo, 44, 24)
	title := " Streams [2026-08-14T10:15:50] "
	showPage(app, "streams", title)

	row := screenRow(t, screen, headerHeight)
	if strings.Contains(row, "[yolo]") {
		t.Errorf("content border row = %q, want no badge over the title", row)
	}
	if !strings.Contains(row, strings.TrimSpace(title)) {
		t.Errorf("content border row = %q, want the whole title %q", row, title)
	}
}

// Only the badge text carries the mode's color; the border it sits in keeps the style file's.
func TestModeBadgeColorsOnlyItself(t *testing.T) {
	app, screen := newBadgeApp(t, config.Yolo, 80, 24)

	pages := app.Layout.PagesRegistry.UI.Pages
	x0, _, width, _ := pages.GetRect()
	_, front := pages.GetFrontPage()
	badge := tview.TaggedStringWidth(modeBadgeText(config.Yolo))
	start, titled := titleStart(front, x0, width)
	if !titled {
		t.Fatalf("the test page has no title to sit beside")
	}
	at := start - badge

	// The badge's own cells, its leading pad aside.
	wantBadge := tcell.GetColor(app.Colors.Skog.Mode.Yolo)
	for x := at + 1; x < at+badge; x++ {
		if got := screenFg(t, screen, x, headerHeight); got != wantBadge {
			t.Fatalf("badge cell %d color = %v, want %v", x, got, wantBadge)
		}
	}

	wantBorder := tcell.GetColor(app.Colors.Skog.Border)
	if got := screenFg(t, screen, x0, headerHeight); got != wantBorder {
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
	if row := screenRow(t, screen, moved); !strings.Contains(row, " [yolo] Profiles ") {
		t.Errorf("row %d = %q, want the badge to have moved there", moved, row)
	}
}

// The badge paints last, so a modal on top of a page cannot cover it.
func TestModeBadgeSurvivesAModal(t *testing.T) {
	app, screen := newBadgeApp(t, config.Yolo, 80, 24)

	pages := app.Layout.PagesRegistry.UI.Pages
	modal := tview.NewBox()
	modal.SetBorder(true).SetTitle(" Opened pages ")
	pages.AddPage(OpenedPages, modal, true, true)
	pages.SendToFront(OpenedPages)
	app.ForceDraw()

	row := screenRow(t, screen, headerHeight)
	if !strings.Contains(row, " [yolo] Opened pages ") {
		t.Errorf("content border row = %q, want the badge on top of the modal", row)
	}
}

// tview.Print reads square brackets as style tags, so an unescaped badge would print as nothing at
// all. What must survive is the printed width: the mode, its brackets and the leading space.
func TestModeBadgeTextEscapesItsBrackets(t *testing.T) {
	for _, mode := range []config.Mode{config.ReadOnly, config.Regular, config.Yolo} {
		text := modeBadgeText(mode)
		if want := len(" [" + string(mode) + "]"); tview.TaggedStringWidth(text) != want {
			t.Errorf(
				"modeBadgeText(%q) prints %d columns, want %d — the brackets were eaten",
				mode,
				tview.TaggedStringWidth(text),
				want,
			)
		}
	}
}
