// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"strings"
	"testing"

	"github.com/rivo/tview"
)

// The help names most of its keys by the binding the bottom bar shows, so that the two cannot
// disagree. A binding renamed out from under it would otherwise print an empty key.
func TestHelpRowsNameBindingsThatExist(t *testing.T) {
	for _, section := range helpSections {
		for _, row := range section.rows {
			switch {
			case row.binding == "" && row.key == "":
				t.Errorf("section %q has a row with neither a binding nor a key", section.title)
			case row.binding != "":
				if _, ok := keys[row.binding]; !ok {
					t.Errorf("section %q names binding %q, which no menu defines",
						section.title, row.binding)
				}
				if row.key != "" {
					t.Errorf("section %q names binding %q and spells a key out as well",
						section.title, row.binding)
				}
			}
			if row.what == "" {
				t.Errorf("section %q has a row that says nothing", section.title)
			}
		}
	}
}

// Every key the help lists has to be readable next to the others, which is what laying them out
// in one column is for.
func TestHelpTextLaysKeysOutInOneColumn(t *testing.T) {
	text := helpText("red", "green", "blue")

	rows := 0
	for _, section := range helpSections {
		if !strings.Contains(text, "[red]"+section.title+"\n") {
			t.Errorf("section %q is missing its heading", section.title)
		}
		for _, row := range section.rows {
			key, what := row.display()
			if key == "" {
				t.Errorf("row %q shows no key", what)
			}
			if !strings.Contains(text, "[green]"+key) {
				t.Errorf("row %q is missing its key %q", what, key)
			}
			if !strings.Contains(text, "[blue]"+what+"\n") {
				t.Errorf("row %q is missing what it does", what)
			}
			rows++
		}
	}

	// A blank line between sections, a heading each, and a line per key.
	want := rows + len(helpSections) + len(helpSections) - 1
	if got := strings.Count(text, "\n"); got != want {
		t.Errorf("the help is %d lines, want %d", got, want)
	}
}

// The modal does not wrap, so a line wider than it is a line cut off. Its width has to hold the
// longest one, the border and the padding included.
func TestHelpWidthHoldsTheLongestLine(t *testing.T) {
	// The border takes a column on each side, the padding another.
	const chrome = 4

	for _, line := range strings.Split(tview.Escape(helpText("", "", "")), "\n") {
		if width := tview.TaggedStringWidth(line); width > helpWidth-chrome {
			t.Errorf("line %q is %d columns wide, over the %d the modal holds",
				line, width, helpWidth-chrome)
		}
	}
}

// The modal is sized for what it holds: a short terminal scrolls the text, a tall one must not
// cut it off.
func TestHelpHeightCountsEveryLine(t *testing.T) {
	text := helpText("", "", "")
	// The lines themselves, plus the two rows the border takes.
	if want := strings.Count(text, "\n") + 2; helpHeight() != want {
		t.Errorf("helpHeight() = %d, want %d", helpHeight(), want)
	}
}
