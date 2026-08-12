// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/sahilm/fuzzy"

	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// Buckets fetches the bucket list of the selected profile and opens it as a page.
func (app *App) Buckets() {
	pageKey := app.bucketsPageKey()
	profile := app.SelectedProfileName()

	fetch(app, "listing buckets",
		func(ctx context.Context, client *s3.Client) ([]s3.Bucket, error) {
			return client.ListBuckets(ctx)
		},
		func(buckets []s3.Bucket) {
			app.showBuckets(pageKey, profile, buckets)
		},
	)
}

func (app *App) showBuckets(pageKey, profile string, buckets []s3.Bucket) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	rows := bucketRows(buckets)
	// visible is what the table currently shows: see showObjects.
	visible := rows

	table := tview.NewTable()
	table.SetSelectable(true, false).
		SetBorder(true).
		SetBorderPadding(0, 0, 1, 0)
	table.SetSelectedStyle(
		tcell.StyleDefault.Foreground(
			tcell.GetColor(app.Colors.Skog.Selection.FgColor),
		).Background(
			tcell.GetColor(app.Colors.Skog.Selection.BgColor),
		),
	)
	table.SetFixed(1, 0)
	fillBucketsTable(table, rows, labelColor)

	title := fmt.Sprintf(" %s:buckets [%d] ", profile, len(rows))
	util.SetSearchableTableTitle(table, title, "")

	selected := func() *bucketRow {
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return nil
		}
		return visible[row-1]
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			// See showObjects: the page being replaced is no longer worth scanning for.
			app.CancelScan()
			Publish(S3Channel, GetBucketsEventType, Payload{nil, true})
			return nil
		}

		if event.Key() == tcell.KeyEsc {
			if app.CancelScan() {
				return nil
			}
			return event
		}

		if IsKey(event, 's') {
			entry := selected()
			if entry == nil {
				return nil
			}

			entry.statCells = scanningCells()
			renderStatCells(table, visible, entry, bucketStatsColumn, entry.statCells)
			// The whole bucket is the empty prefix.
			app.ScanPrefix(entry.name, "", func(stats *s3.PrefixStats) {
				entry.statCells = cellsOf(stats)
				renderStatCells(table, visible, entry, bucketStatsColumn, entry.statCells)
			})
			return nil
		}

		if event.Key() == tcell.KeyEnter {
			entry := selected()
			if entry == nil {
				return nil
			}
			Publish(
				S3Channel,
				GetObjectsEventType,
				Payload{ObjectsTarget{Bucket: entry.name}, false},
			)
			return nil
		}

		return event
	})

	app.AddToPagesRegistry(pageKey, table, BucketsPageMenu, true)

	app.AssignSearch(func(text string) {
		visible = filterBucketRows(rows, text)
		fillBucketsTable(table, visible, labelColor)
		util.SetSearchableTableTitle(table, title, text)
		table.ScrollToBeginning()
	})
}

// bucketRow is one displayed bucket. Rows are held by pointer for the same reason objectRow is: an
// aggregate scanned into one has to survive a re-render.
type bucketRow struct {
	name    string
	created string
	region  string
	statCells
}

// bucketRows renders the bucket list as display rows. Their aggregate cells start out unknown:
// what a bucket holds costs a walk of every key in it.
func bucketRows(buckets []s3.Bucket) []*bucketRow {
	rows := make([]*bucketRow, 0, len(buckets))

	for _, bucket := range buckets {
		region := bucket.Region
		if region == "" {
			region = unknownCell
		}
		rows = append(rows, &bucketRow{
			name:      bucket.Name,
			created:   s3.FormatTime(bucket.CreatedAt),
			region:    region,
			statCells: unknownCells(),
		})
	}

	return rows
}

// bucketStatsColumn is the first aggregate column of the buckets table.
const bucketStatsColumn = 3

// fillBucketsTable rebuilds the table rows, headers included, and puts the cursor on the
// first bucket.
func fillBucketsTable(table *tview.Table, rows []*bucketRow, labelColor tcell.Color) {
	table.Clear()
	util.SetTableHeaders(
		table,
		labelColor,
		"Name",
		"Created",
		"Region",
		"Objects",
		"Size",
		"Last-modified",
		"Storage-class",
	)

	for i, entry := range rows {
		row := i + 1
		table.
			SetCell(row, 0, tview.NewTableCell(entry.name)).
			SetCell(row, 1, tview.NewTableCell(entry.created)).
			SetCell(row, 2, tview.NewTableCell(entry.region)).
			SetCell(row, 3, tview.NewTableCell(entry.objects)).
			SetCell(row, 4, tview.NewTableCell(entry.size)).
			SetCell(row, 5, tview.NewTableCell(entry.lastModified)).
			SetCell(row, 6, tview.NewTableCell(entry.storageClass))
	}

	if table.GetRowCount() > 1 {
		table.Select(1, 0)
	}
}

// filterBucketRows keeps the buckets whose name fuzzy-matches the filter, in match order.
func filterBucketRows(rows []*bucketRow, filter string) []*bucketRow {
	if filter == "" {
		return rows
	}

	names := make([]string, len(rows))
	for i, entry := range rows {
		names[i] = entry.name
	}

	matches := fuzzy.Find(filter, names)
	filtered := make([]*bucketRow, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, rows[match.Index])
	}
	return filtered
}
