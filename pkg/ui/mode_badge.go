// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/config"
)

// modeBadgeMargin leaves the frame's top-left corner rune alone.
const modeBadgeMargin = 1

// titleStart is the first column tview puts the front page's centred title in, mirroring what
// Box.Draw does: the title is printed into the box interior and centred there. It reports false
// for a page with no title, which has no anchor to put the badge against.
//
// The badge is painted immediately to its left, so that the mode and the title read as one label
// rather than as two things stranded at opposite ends of the same line.
func titleStart(front tview.Primitive, x, width int) (int, bool) {
	titled, ok := front.(interface{ GetTitle() string })
	if !ok || titled.GetTitle() == "" {
		return 0, false
	}

	// Halved separately, exactly as tview.Print does for AlignCenter — rounding them together
	// lands a column to the left and puts a border rune between the badge and the title.
	interior, title := width-2, tview.TaggedStringWidth(titled.GetTitle())
	if title >= interior {
		return x + 1, true
	}
	return x + 1 + interior/2 - title/2, true
}

// modeBadgeText is what the badge says. The mode is always named, the default one included: an
// empty corner would otherwise be indistinguishable from a badge that simply did not draw.
//
// It is padded on the left only. The page title it is painted against carries its own leading
// space, which is the gap between the two.
//
// The brackets go through tview.Escape: tview.Print reads a bare "[yolo]" as a style tag and
// prints nothing at all. TaggedStringWidth, which the caller measures with, counts the escaped
// form at its printed width, so the positioning arithmetic is unaffected.
func modeBadgeText(mode config.Mode) string {
	return " " + tview.Escape("["+string(mode)+"]")
}

// modeColor is the color the badge is painted in. Only yolo is coloured, and no style file can
// change that: it is the one mode that deletes without asking, and a theme must not be able to
// hide the warning. Every other mode takes the border's own color.
func modeColor(mode config.Mode) tcell.Color {
	if mode == config.Yolo {
		return tcell.ColorRed
	}
	return tcell.ColorDefault
}

// drawModeBadge paints the mode into the top border line of the content area, immediately to the
// left of the page title.
//
// It is installed with SetAfterDrawFunc rather than as a draw function on the frame: Flex and Pages
// both draw their own box before their children, so anything painted there would be covered by the
// front page's own border. This has to land on top of it, which means painting last.
//
// It must stay pure painting. It runs inside Application.draw() with the application lock held, so
// queueing an update, forcing a draw or sending a status from here deadlocks the UI. It reads the
// selected profile's mode without a lock, which is safe as long as every draw and every mode change
// happens on the UI goroutine — true while nothing calls Suspend.
func (app *App) drawModeBadge(screen tcell.Screen) {
	if app.Layout == nil {
		return
	}
	pages := app.Layout.PagesRegistry.UI.Pages

	// The first frame has no page yet — the Profiles page arrives on a queued update — and so no
	// border line to paint into. A page is assumed to have a border, as every page of skog does.
	name, front := pages.GetFrontPage()
	if name == "" {
		return
	}

	// Below 2x2 a box draws no border at all.
	x, y, width, height := pages.GetRect()
	if width < 2 || height < 2 {
		return
	}

	mode := app.Mode()
	label := modeBadgeText(mode)
	badge := tview.TaggedStringWidth(label)

	// Left of the title, and never over the corner: on a terminal too narrow to hold both, the
	// title is what the user needs and the badge stays off. A page with no title has nothing to
	// sit beside, so the badge goes where it would otherwise have started.
	at := x + modeBadgeMargin
	if start, titled := titleStart(front, x, width); titled {
		at = start - badge
	}
	if at < x+modeBadgeMargin {
		return
	}

	tview.Print(
		screen,
		label,
		at,
		y,
		badge,
		tview.AlignLeft,
		modeColor(mode),
	)
}
