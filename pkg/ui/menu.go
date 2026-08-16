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
	"upd": {
		Key:   "<C-u>",
		Value: "Update",
	},
	"stat": {
		Key:   "<i>",
		Value: "Info",
	},
	"more": {
		Key:   "<n>",
		Value: "Next batch",
	},
	// The hierarchy keys. A page that has only one level, or none, shows only the key that
	// leads somewhere from it.
	"updown": {
		Key:   "<h/l>",
		Value: "Up/Open",
	},
	"up": {
		Key:   "<h>",
		Value: "Up",
	},
	"down": {
		Key:   "<l>",
		Value: "Open",
	},
	"hlscroll": {
		Key:   "<H,L>",
		Value: "Scroll Left/Right",
	},
	"delete": {
		Key:   "<x>",
		Value: "Delete",
	},
	"download": {
		Key:   "<d>",
		Value: "Download",
	},
	"schema": {
		Key:   "<s>",
		Value: "Schema",
	},
	"mode": {
		Key:   "<Tab>",
		Value: "Mode",
	},
	"close": {
		Key:   "<Esc>",
		Value: "Close",
	},
	"help": {
		Key:   "<?>",
		Value: "Keys",
	},
}

// Page menu identifiers, used as keys into the map passed to NewMenu to look up
// the keybindings shown for the currently active page.
const (
	ResourcesPageMenu = "ResourcesPageMenu"
	ProfilesPageMenu  = "ProfilesPageMenu"
	BucketsPageMenu   = "BucketsPageMenu"
	ObjectsPageMenu   = "ObjectsPageMenu"
	// ObjectsBatchedPageMenu is the objects menu of a level with a batch still to load, and so
	// the only one offering <n>.
	ObjectsBatchedPageMenu    = "ObjectsBatchedPageMenu"
	ObjectDescriptionPageMenu = "ObjectDescriptionPageMenu"
	// ObjectRowsPageMenu is the menu of a file shown as a table. Only a file with a schema is,
	// so these are the menus offering <s>.
	ObjectRowsPageMenu = "ObjectRowsPageMenu"
	// ObjectRowsBatchedPageMenu is the table menu of a file with rows still to read, and so
	// one of the two offering <n>.
	ObjectRowsBatchedPageMenu = "ObjectRowsBatchedPageMenu"
	// ObjectLinesPageMenu is the menu of a file shown as its lines, which declares no schema.
	ObjectLinesPageMenu = "ObjectLinesPageMenu"
	// ObjectLinesBatchedPageMenu is the lines menu of a file with lines still to read.
	ObjectLinesBatchedPageMenu = "ObjectLinesBatchedPageMenu"
	// ObjectSchemaPageMenu is the menu of a schema page.
	ObjectSchemaPageMenu = "ObjectSchemaPageMenu"
	// IcebergBucketsPageMenu is the bucket list of the Iceberg resource.
	IcebergBucketsPageMenu = "IcebergBucketsPageMenu"
	// IcebergLevelPageMenu is one level of a warehouse: namespaces and tables.
	IcebergLevelPageMenu = "IcebergLevelPageMenu"
	// IcebergLevelBatchedPageMenu is that level with a batch still to load.
	IcebergLevelBatchedPageMenu = "IcebergLevelBatchedPageMenu"
	// IcebergTablePageMenu is a table's metadata documents, the page offering <i> for the table
	// itself and <s> for the schema of the version under the cursor.
	IcebergTablePageMenu = "IcebergTablePageMenu"
	// IcebergOverviewPageMenu is the table overview.
	IcebergOverviewPageMenu = "IcebergOverviewPageMenu"
	// IcebergSchemaPageMenu is the schema of a metadata version.
	IcebergSchemaPageMenu = "IcebergSchemaPageMenu"
	// IcebergSnapshotsPageMenu is the snapshots of a metadata version.
	IcebergSnapshotsPageMenu = "IcebergSnapshotsPageMenu"
	// IcebergSnapshotsBatchedPageMenu is that page with snapshots still to show.
	IcebergSnapshotsBatchedPageMenu = "IcebergSnapshotsBatchedPageMenu"
	// IcebergManifestsPageMenu is the manifests of a snapshot.
	IcebergManifestsPageMenu = "IcebergManifestsPageMenu"
	// IcebergFilesPageMenu is the files of a manifest. Deleting is absent from it and from every
	// other Iceberg page: a file removed from under a table breaks it.
	IcebergFilesPageMenu = "IcebergFilesPageMenu"
	// IcebergFilesBatchedPageMenu is that page with files still to show.
	IcebergFilesBatchedPageMenu = "IcebergFilesBatchedPageMenu"
	// RecordPageMenu is the menu of the popup showing one row in full.
	RecordPageMenu = "RecordPageMenu"
	// HelpPageMenu is the menu of the modal listing every key.
	HelpPageMenu = "HelpPageMenu"
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
			ProfilesPageMenu: {
				"sel",
				"select",
				"mode",
				"res",
				"down",
			},
			BucketsPageMenu: {
				"sel",
				"stat",
				"res",
				"search",
				"upd",
				"updown",
			},
			ObjectsPageMenu: {
				"sel",
				"stat",
				"download",
				"delete",
				"res",
				"search",
				"upd",
				"updown",
			},
			ObjectsBatchedPageMenu: {
				"sel",
				"stat",
				"more",
				"download",
				"delete",
				"res",
				"search",
				"upd",
				"updown",
			},
			ObjectDescriptionPageMenu: {
				"res",
				"hlscroll",
				"upd",
				"updown",
			},
			ObjectRowsPageMenu: {
				"sel",
				"schema",
				"hlscroll",
				"res",
				"search",
				"upd",
				"updown",
			},
			ObjectRowsBatchedPageMenu: {
				"sel",
				"schema",
				"more",
				"hlscroll",
				"res",
				"search",
				"upd",
				"updown",
			},
			ObjectLinesPageMenu: {
				"sel",
				"hlscroll",
				"res",
				"search",
				"upd",
				"up",
			},
			ObjectLinesBatchedPageMenu: {
				"sel",
				"more",
				"hlscroll",
				"res",
				"search",
				"upd",
				"up",
			},
			ObjectSchemaPageMenu: {
				"res",
				"hlscroll",
				"up",
			},
			IcebergBucketsPageMenu: {
				"sel",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergLevelPageMenu: {
				"sel",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergLevelBatchedPageMenu: {
				"sel",
				"more",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergTablePageMenu: {
				"sel",
				"stat",
				"schema",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergOverviewPageMenu: {
				"res",
				"hlscroll",
				"upd",
				"up",
			},
			IcebergSchemaPageMenu: {
				"sel",
				"res",
				"search",
				"upd",
				"up",
			},
			IcebergSnapshotsPageMenu: {
				"sel",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergSnapshotsBatchedPageMenu: {
				"sel",
				"more",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergManifestsPageMenu: {
				"sel",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergFilesPageMenu: {
				"sel",
				"download",
				"res",
				"search",
				"upd",
				"updown",
			},
			IcebergFilesBatchedPageMenu: {
				"sel",
				"more",
				"download",
				"res",
				"search",
				"upd",
				"updown",
			},
			RecordPageMenu: {
				"sel",
				"up",
				"close",
			},
			HelpPageMenu: {
				"sel",
				"close",
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
