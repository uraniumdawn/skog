// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/util"
)

// helpRow is one key of the help modal.
//
// A key that a page menu offers is named by its binding rather than written out, so the help
// and the bottom bar can never disagree about which key does what. The few keys no menu shows —
// the answer to a question, the way out of the application — carry their own text.
type helpRow struct {
	binding string
	key     string
	what    string
}

// display returns what the row shows: its key, and what that key does.
func (r helpRow) display() (string, string) {
	if r.binding == "" {
		return r.key, r.what
	}
	return keys[r.binding].Key, r.what
}

// helpSection is a group of keys under a heading.
type helpSection struct {
	title string
	rows  []helpRow
}

// helpSections is what <?> shows. The order is the order a session goes in: moving about first,
// then what a page offers, then what a row does, then the application itself.
var helpSections = []helpSection{
	{"Moving", []helpRow{
		{binding: "updown", what: "Level up, or into the row under the cursor"},
		{binding: "sel", what: "Move between rows"},
		{key: "<g/G>", what: "First row, last row"},
		{key: "<C-f/C-b>", what: "Page down, page up"},
		{binding: "hlscroll", what: "Scroll a wide page sideways"},
	}},
	{"Pages", []helpRow{
		{binding: "res", what: "Open the resources: profiles, S3, Iceberg"},
		{binding: "search", what: "Filter the rows of the page"},
		{binding: "upd", what: "Fetch the page again"},
		{key: "<Esc>", what: "Cancel the running job, or close a modal"},
	}},
	{"Rows", []helpRow{
		{binding: "stat", what: "What the row holds: a folder walked, an object opened"},
		{binding: "download", what: "Download the row, a folder and all under it"},
		{binding: "delete", what: "Delete the object, or the folder and all under it"},
		{binding: "more", what: "Load the next batch of a level or a file"},
		{binding: "schema", what: "The schema the file being viewed declares"},
	}},
	{"Iceberg", []helpRow{
		{binding: "down", what: "Table, snapshots, manifests, files, rows"},
		{binding: "stat", what: "What a metadata version says about the table"},
		{binding: "schema", what: "The schema that metadata version declares"},
	}},
	{"Profiles", []helpRow{
		{binding: "select", what: "Work against the profile under the cursor"},
		{binding: "mode", what: "Switch it between read-only, regular and yolo"},
	}},
	{"Application", []helpRow{
		{binding: "help", what: "This list"},
		{key: "<Y/N>", what: "Answer the question in the status line"},
		{key: "<C-c>", what: "Quit"},
	}},
}

// NewHelpPage builds the modal <?> shows. It is built once and shown as it is, since the keys
// it lists are the same on every page.
func (app *App) NewHelpPage() tview.Primitive {
	view := tview.NewTextView()
	view.SetDynamicColors(true).
		SetWrap(false)
	view.SetBorder(true).
		SetBorderPadding(0, 0, 1, 1).
		SetTitle(" Keys ")
	view.SetBackgroundColor(tcell.GetColor(app.Colors.Skog.Background))
	view.SetText(helpText(app.Colors.Skog.Label.FgColor,
		app.Colors.Skog.Keybinding.Key, app.Colors.Skog.Keybinding.Value))

	// <?> closes it too, but that one is answered by the application-wide capture, which runs
	// before this one.
	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			app.HideHelp()
			return nil
		}
		return event
	})

	return util.NewHelpModal(view, helpWidth, helpHeight())
}

// helpWidth is how wide the modal is: the longest line it holds, and room for the border.
const helpWidth = 74

// helpHeight is how tall the modal is: a line per row, a heading and a blank line per section,
// and the border. A terminal too short for it scrolls the text rather than losing rows.
func helpHeight() int {
	height := 2
	for _, section := range helpSections {
		height += len(section.rows) + 2
	}
	// The last section is followed by no blank line.
	return height - 1
}

// helpText lays the sections out, the keys of all of them in one column so that what they do
// lines up down the modal.
func helpText(headingColor, keyColor, valueColor string) string {
	width := 0
	for _, section := range helpSections {
		for _, row := range section.rows {
			if key, _ := row.display(); utf8.RuneCountInString(key) > width {
				width = utf8.RuneCountInString(key)
			}
		}
	}

	var out strings.Builder
	for i, section := range helpSections {
		if i > 0 {
			out.WriteString("\n")
		}
		fmt.Fprintf(&out, "[%s]%s\n", headingColor, section.title)

		for _, row := range section.rows {
			key, what := row.display()
			fmt.Fprintf(
				&out,
				"[%s]%s%s  [%s]%s\n",
				keyColor,
				key,
				strings.Repeat(" ", width-utf8.RuneCountInString(key)),
				valueColor,
				what,
			)
		}
	}
	return out.String()
}

// ShowHelp opens the key list over whatever page is in front.
func (app *App) ShowHelp() {
	app.ShowModalPage(Help)
	// Focus follows the page in front, so the keys the modal answers to reach it rather than
	// the page it is covering.
	app.SetFocus(app.Layout.PagesRegistry.UI.Pages)
}

// HideHelp closes the key list, back to the page it was opened over.
func (app *App) HideHelp() {
	app.HideModalPage(Help)
	app.SetFocus(app.Layout.PagesRegistry.UI.Pages)
}
