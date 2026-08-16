// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package util provides utility functions for the skog application.
package util

import (
	"fmt"
	"slices"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// SetTableHeaders sets row 0 of table to non-selectable header cells with the given
// labelColor, one cell per entry in headers, in column order.
func SetTableHeaders(table *tview.Table, labelColor tcell.Color, headers ...string) {
	for col, text := range headers {
		table.SetCell(0, col, tview.NewTableCell(text).SetSelectable(false).SetTextColor(labelColor))
	}
}

// NewHelpModal centers p in a flex layout of a fixed size, sized for the key list. A terminal
// too small for it gets what fits, which is what tview does with any item it cannot lay out
// whole.
func NewHelpModal(p tview.Primitive, width, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 1, false), width, 0, true).
		AddItem(nil, 0, 1, false)
}

// NewRecordModal centers p in a flex layout sized for reading one record: most of the screen,
// since what is opened in it is what did not fit on the page behind it.
func NewRecordModal(p tview.Primitive) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, 0, 10, true).
			AddItem(nil, 0, 1, false), 0, 10, true).
		AddItem(nil, 0, 1, false)
}

// NewResourceModal centers p in a flex layout with a fixed height, sized for resource forms.
func NewResourceModal(p tview.Primitive, height int) tview.Primitive {
	return tview.NewFlex().
		AddItem(nil, 0, 2, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 1, 0, false).
			AddItem(p, height, 0, true).
			AddItem(nil, 0, 9, false), 0, 2, true).
		AddItem(nil, 2, 0, false)
}

// BuildTitle creates a formatted title string from parts separated by colons.
func BuildTitle(parts ...string) string {
	var builder strings.Builder
	builder.WriteString(" ")
	for i, part := range parts {
		builder.WriteString(strings.ToLower(part))
		if i < len(parts)-1 {
			builder.WriteString(":")
		}
	}
	builder.WriteString(" ")
	return builder.String()
}

// BuildPageKey creates a page key string from parts separated by colons.
func BuildPageKey(parts ...string) string {
	var builder strings.Builder
	for i, part := range parts {
		builder.WriteString(strings.ToLower(part))
		if i < len(parts)-1 {
			builder.WriteString(":")
		}
	}
	return builder.String()
}

// Titled is anything drawn in a bordered box with a title: a table, a text view.
type Titled interface {
	SetTitle(string) *tview.Box
}

// SetSearchableTitle sets the title of a page with an optional filter appended, so a filtered
// page says what it is filtered by.
func SetSearchableTitle(p Titled, title, filter string) {
	if filter != "" {
		p.SetTitle(fmt.Sprintf("%s[grey]/%s ", title, filter))
	} else {
		p.SetTitle(title)
	}
}

// SortedKeys returns the keys of m in ascending order, so output built from a map does not
// shuffle between renderings of the same data.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
