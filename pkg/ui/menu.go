// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"fmt"

	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/config"
)

// Menu renders the bottom keybinding bar for the currently active page.
type Menu struct {
	Content *tview.Table
	Flex    *tview.Flex
	Map     *map[string]*[]string
	Colors  *config.ColorConfig
}

// Pair holds a keybinding's display key and description.
type Pair struct {
	Key   string
	Value string
}

var keys = map[string]Pair{
	"sel": {
		Key:   "<j/↓,k/↑>",
		Value: "Move",
	},
	"select": {
		Key:   "<Enter>",
		Value: "Select",
	},
	"open": {
		Key:   "<Enter>",
		Value: "Open",
	},
	"res": {
		Key:   "<:>",
		Value: "Resources",
	},
	"resource_search": {
		Key:   "<:>",
		Value: "Search",
	},
	"search": {
		Key:   "</>",
		Value: "Search",
	},
	"opened": {
		Key:   "<C-p>",
		Value: "Opened Pages",
	},
	"upd": {
		Key:   "<C-u>",
		Value: "Update",
	},
	"stat": {
		Key:   "<s>",
		Value: "Size",
	},
	"more": {
		Key:   "<n>",
		Value: "Next batch",
	},
	"forward": {
		Key:   "<l>",
		Value: "Forward",
	},
	"b/f": {
		Key:   "<h/l>",
		Value: "Backward/Forward",
	},
	"hlscroll": {
		Key:   "<H,L>",
		Value: "Scroll Left/Right",
	},
	"remove_page": {
		Key:   "<x>",
		Value: "Remove page",
	},
	"close": {
		Key:   "<Esc>",
		Value: "Close",
	},
	"esc_confirm_opened": {
		Key:   "<Esc, Enter>",
		Value: "Confirm and back",
	},
}

// Page menu identifiers, used as keys into the map passed to NewMenu to look up
// the keybindings shown for the currently active page.
const (
	ResourcesPageMenu = "ResourcesPageMenu"
	OpenedPagesMenu   = "OpenedPagesMenu"
	ProfilesPageMenu  = "ProfilesPageMenu"
	BucketsPageMenu   = "BucketsPageMenu"
	ObjectsPageMenu   = "ObjectsPageMenu"
	// ObjectsBatchedPageMenu is the objects menu of a level with a batch still to load, and so
	// the only one offering <n>.
	ObjectsBatchedPageMenu    = "ObjectsBatchedPageMenu"
	ObjectDescriptionPageMenu = "ObjectDescriptionPageMenu"
)

// NewMenu builds the keybinding bar, pre-rendering the keybinding rows for every
// PageMenu it knows about.
func NewMenu(colors *config.ColorConfig) *Menu {
	table := tview.NewTable().
		SetSelectable(false, false)

	flex := tview.NewFlex().SetDirection(tview.FlexColumn)
	flex.AddItem(table, 0, 1, true)

	return &Menu{
		Content: table,
		Flex:    flex,
		Map: &map[string]*[]string{
			ResourcesPageMenu: {
				"sel",
				"resource_search",
				"select",
				"close",
			},
			OpenedPagesMenu: {
				"sel",
				"search",
				"remove_page",
				"esc_confirm_opened",
			},
			ProfilesPageMenu: {
				"sel",
				"select",
				"res",
				"opened",
				"forward",
			},
			BucketsPageMenu: {
				"sel",
				"open",
				"stat",
				"res",
				"search",
				"upd",
				"opened",
				"b/f",
			},
			ObjectsPageMenu: {
				"sel",
				"open",
				"stat",
				"res",
				"search",
				"upd",
				"opened",
				"b/f",
			},
			ObjectsBatchedPageMenu: {
				"sel",
				"open",
				"stat",
				"more",
				"res",
				"search",
				"upd",
				"opened",
				"b/f",
			},
			ObjectDescriptionPageMenu: {
				"res",
				"hlscroll",
				"opened",
				"upd",
				"b/f",
			},
		},
		Colors: colors,
	}
}

// SetMenu redraws the keybinding bar with the entries registered for the given PageMenu.
func (m *Menu) SetMenu(menu string) {
	m.Content.Clear()
	if keyBindings, ok := (*m.Map)[menu]; ok {
		row := 0
		col := 0
		// Keep this in step with headerHeight: the menu must fit inside the header.
		maxRowsPerColumn := headerHeight

		for _, binding := range *keyBindings {
			if value, exists := keys[binding]; exists {
				keyColor := m.Colors.Skog.Keybinding.Key
				valueColor := m.Colors.Skog.Keybinding.Value

				// Calculate the current column offset (each column takes 2 cells: key and value)
				colOffset := col * 2

				m.Content.SetCell(
					row,
					colOffset,
					tview.NewTableCell(fmt.Sprintf("[%s]%s", keyColor, value.Key)),
				)
				m.Content.SetCell(
					row,
					colOffset+1,
					tview.NewTableCell(fmt.Sprintf("[%s]%s", valueColor, value.Value)),
				)

				row++

				// If we've reached the max rows per column, move to the next column
				if row >= maxRowsPerColumn {
					row = 0
					col++
				}
			}
		}
	}
}
