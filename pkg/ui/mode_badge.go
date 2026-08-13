// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/config"
)

const (
	// modeBadgeMargin leaves the frame's top-right corner rune alone.
	modeBadgeMargin = 1
	// modeBadgeTitleRoom is how many columns of page title must survive to the left of the badge.
	// On anything narrower the title is what the user needs, so the badge stays off.
	modeBadgeTitleRoom = 16
)

// modeBadgeText is what the badge says. The mode is always named, the default one included: an
// empty corner would otherwise be indistinguishable from a badge that simply did not draw.
//
// It carries no square brackets: tview.Print reads those as style tags, and a bracketed mode would
// print as nothing at all.
func modeBadgeText(mode config.Mode) string {
	return " Mode: " + string(mode) + " "
}

// modeColor is the color the badge is painted in, from the style file.
func (app *App) modeColor(mode config.Mode) string {
	switch mode {
	case config.ReadOnly:
		return app.Colors.Skog.Mode.ReadOnly
	case config.Yolo:
		return app.Colors.Skog.Mode.Yolo
	default:
		return app.Colors.Skog.Mode.Regular
	}
}

// drawModeBadge paints the mode into the top border line of the content area, at the right end of
// the line the page title starts.
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
	if name, _ := pages.GetFrontPage(); name == "" {
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
	if width < badge+modeBadgeMargin+modeBadgeTitleRoom+1 {
		return
	}

	tview.Print(
		screen,
		label,
		x+width-modeBadgeMargin-badge,
		y,
		badge,
		tview.AlignLeft,
		tcell.GetColor(app.modeColor(mode)),
	)
}
