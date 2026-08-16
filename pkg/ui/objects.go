// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/sahilm/fuzzy"

	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// Objects fetches one level of a bucket's key hierarchy and opens it as a page.
//
// S3 keys are flat, so a "folder" is just the part of a key up to the next delimiter. Opening
// a folder lists the next level down, which is what Enter does on a row that ends with the
// delimiter; Enter on any other row opens that object's metadata.
func (app *App) Objects(target ObjectsTarget) {
	pageKey := app.objectsPageKey(target)
	path := s3.DisplayPath(target.Bucket, target.Prefix)

	fetch(app, "listing "+path,
		func(ctx context.Context, client *s3.Client) (*s3.Listing, error) {
			return client.ListObjects(ctx, target.Bucket, target.Prefix)
		},
		func(listing *s3.Listing) {
			app.showObjects(pageKey, path, listing)
		},
	)
}

// deleteObject removes one object and calls done on the UI goroutine once S3 has accepted it.
func (app *App) deleteObject(bucket, key string, done func()) {
	path := s3.DisplayPath(bucket, key)

	perform(app, "deleting "+path, "deleted "+path,
		func(ctx context.Context, client *s3.Client) error {
			return client.DeleteObject(ctx, bucket, key)
		},
		done,
	)
}

// deletePrefix removes every key under a folder and calls done on the UI goroutine once the
// level is gone. It holds the job slot for as long as it runs — a level takes one request per
// thousand keys, which the single-call timeout does not allow for — so it is <Esc> that stops
// it; see job.go.
//
// A delete that did not finish leaves the page showing keys that are no longer there, and which
// ones went is not known here: the level is left as it is and the user is told to refresh it.
func (app *App) deletePrefix(bucket, prefix string, done func()) {
	path := s3.DisplayPath(bucket, prefix)

	if !app.beginJob("a delete") {
		return
	}

	ctx, cancel := context.WithCancel(app.ctx)
	app.setJobCancel(cancel)

	SendStatusInfinite("deleting " + path + " (<Esc> to cancel)")

	go func() {
		defer cancel()
		defer app.endJob()

		client, err := app.S3Client(ctx)
		if err != nil {
			failed("deleting "+path, err)
			return
		}

		result, err := client.DeletePrefix(ctx, bucket, prefix, deleteProgress(path))
		switch {
		// A cancelled delete surfaces as whatever the transport made of it, so the context is
		// what says the user is the one who ended it.
		case err != nil && (errors.Is(err, context.Canceled) || ctx.Err() != nil):
			SendStatusWithDefaultTTL(fmt.Sprintf(
				"delete of %s cancelled, %s objects gone; <C-u> to refresh the level",
				path,
				util.FormatNumber(int64(result.Objects)),
			))
		case err != nil:
			failed(fmt.Sprintf(
				"deleting %s (%s objects gone; <C-u> to refresh the level)",
				path,
				util.FormatNumber(int64(result.Objects)),
			), err)
		default:
			SendStatusWithDefaultTTL(fmt.Sprintf(
				"deleted %s: %s objects",
				path,
				util.FormatNumber(int64(result.Objects)),
			))
			app.QueueUpdateDraw(done)
		}
	}()
}

// deleteProgress reports how far a recursive delete has got. A batch is up to a thousand keys,
// so a report per batch is a report worth redrawing the status line for.
func deleteProgress(path string) func(int) {
	return func(done int) {
		SendStatusInfinite(fmt.Sprintf(
			"deleting %s — %s objects (<Esc> to cancel)",
			path,
			util.FormatNumber(int64(done)),
		))
	}
}

// moreObjects loads the next batch of an opened level and hands it to add.
func (app *App) moreObjects(
	path string,
	target ObjectsTarget,
	token string,
	add func(*s3.Listing),
) {
	fetch(app, "listing more of "+path,
		func(ctx context.Context, client *s3.Client) (*s3.Listing, error) {
			return client.ListMore(ctx, target.Bucket, target.Prefix, token)
		},
		add,
	)
}

func (app *App) showObjects(pageKey, path string, listing *s3.Listing) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	target := ObjectsTarget{Bucket: listing.Bucket, Prefix: listing.Prefix}
	rows := objectRows(listing)
	// visible is what the table currently shows: the rows themselves under no filter, a subset
	// under one. It is how a table row maps back to the row a keypress acts on.
	visible := rows
	// token continues the level past the batch on screen, and is empty once it is all loaded.
	token := listing.NextToken

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
	fillObjectsTable(table, rows, labelColor)

	title := objectsTitle(path, len(rows))
	util.SetSearchableTitle(table, title, "")

	// render redraws the table for the filter in force, which is also what a new batch needs.
	render := func(filter string) {
		visible = filterObjectRows(rows, filter)
		fillObjectsTable(table, visible, labelColor)
		title = objectsTitle(path, len(rows))
		util.SetSearchableTitle(table, title, filter)
	}

	// selected returns the row the cursor is on, nil on the header or an empty table.
	selected := func() *objectRow {
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return nil
		}
		return visible[row-1]
	}

	// open descends into the row the cursor is on: a folder lists the level under it, an object
	// opens its metadata.
	open := func() {
		entry := selected()
		if entry == nil {
			return
		}

		if entry.folder {
			Publish(
				S3Channel,
				GetObjectsEventType,
				Payload{ObjectsTarget{Bucket: listing.Bucket, Prefix: entry.full}, false},
			)
			return
		}
		Publish(
			S3Channel,
			GetObjectEventType,
			Payload{ObjectTarget{Bucket: listing.Bucket, Key: entry.full}, false},
		)
	}

	// up opens the level above this one: the prefix it hangs off, or the bucket list when this
	// is the root of a bucket.
	up := func() {
		parent, ok := s3.Parent(listing.Prefix)
		if !ok {
			Publish(S3Channel, GetBucketsEventType, Payload{nil, false})
			return
		}
		Publish(
			S3Channel,
			GetObjectsEventType,
			Payload{ObjectsTarget{Bucket: listing.Bucket, Prefix: parent}, false},
		)
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			// The refreshed page starts with no aggregates, so a scan filling in the rows of the
			// page being replaced has nothing left to fill.
			app.CancelJob()
			Publish(
				S3Channel,
				GetObjectsEventType,
				Payload{ObjectsTarget{Bucket: listing.Bucket, Prefix: listing.Prefix}, true},
			)
			return nil
		}

		if event.Key() == tcell.KeyEsc {
			if app.CancelJob() {
				return nil
			}
			return event
		}

		if IsKey(event, 'n') {
			// <n> is off the menu once the level is complete, so this only guards a stale keypress.
			if token == "" {
				return nil
			}

			app.moreObjects(path, target, token, func(next *s3.Listing) {
				rows = appendBatch(rows, next)
				token = next.NextToken
				render(app.CurrentFilters[pageKey])
				app.SetPageMenu(pageKey, objectsMenu(token))
			})
			return nil
		}

		if IsKey(event, 'i') {
			entry := selected()
			if entry == nil {
				return nil
			}
			// <i> is what tells the user about the row under the cursor, whatever the row is.
			// A folder is told about in place, by walking what it holds; an object has nothing
			// to fill in — the listing filled its cells already — so what is left to tell is
			// its metadata, which is a page of its own.
			if !entry.folder {
				Publish(
					S3Channel,
					GetObjectEventType,
					Payload{ObjectTarget{Bucket: listing.Bucket, Key: entry.full}, false},
				)
				return nil
			}

			entry.statCells = scanningCells()
			renderStatCells(table, visible, entry, objectStatsColumn, entry.statCells)
			app.ScanPrefix(listing.Bucket, entry.full, func(stats *s3.PrefixStats) {
				entry.statCells = cellsOf(stats)
				renderStatCells(table, visible, entry, objectStatsColumn, entry.statCells)
			})
			return nil
		}

		if IsKey(event, 'd') {
			entry := selected()
			if entry == nil {
				return nil
			}

			// A folder brings every key under it along; an object is just itself.
			app.Download(listing.Bucket, entry.full, entry.folder)
			return nil
		}

		if IsKey(event, 'x') {
			entry := selected()
			if entry == nil {
				return nil
			}
			key := entry.full
			folder := entry.folder
			// The row the cursor is on now, so deleting several keys one after another does not
			// send it back to the top of the level on every re-render.
			row, _ := table.GetSelection()

			// Whatever went, the row it was on goes with it.
			gone := func() {
				rows = withoutKey(rows, key)
				render(app.CurrentFilters[pageKey])
				if count := table.GetRowCount(); count > 1 {
					if row >= count {
						row = count - 1
					}
					table.Select(row, 0)
				}
			}

			// The question names no key: one can be long enough to push the answer off the
			// status line, and the row it acts on is the highlighted one anyway. What it does
			// say is how far the delete reaches — a folder takes every key under it, however
			// deep, and S3 has no undo.
			question := "Delete the selected object?"
			if folder {
				question = "Delete the selected folder and everything under it?"
			}

			app.Modify(question, func() {
				if folder {
					app.deletePrefix(listing.Bucket, key, gone)
					return
				}
				app.deleteObject(listing.Bucket, key, gone)
			})
			return nil
		}

		return event
	})

	app.AddToPagesRegistry(pageKey, table, objectsMenu(token), true)
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, up, open)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})
}

// objectsTitle names the level and how many of its entries are loaded. That a batch is left to
// load is said by <n> being on the menu, not here.
func objectsTitle(path string, loaded int) string {
	return fmt.Sprintf(" %s [%d] ", path, loaded)
}

// objectsMenu is the menu of a level: the one offering <n> only while a next batch exists.
func objectsMenu(token string) string {
	if token == "" {
		return ObjectsPageMenu
	}
	return ObjectsBatchedPageMenu
}

// appendBatch adds a batch to the rows already on the page, keeping folders ahead of objects and
// each group in key order. Existing rows are kept as they are, aggregates included.
func appendBatch(rows []*objectRow, batch *s3.Listing) []*objectRow {
	rows = append(rows, objectRows(batch)...)
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].folder != rows[j].folder {
			return rows[i].folder
		}
		return rows[i].full < rows[j].full
	})
	return rows
}

// withoutKey drops the row holding the given key, leaving the rest in place. A page never holds
// the same key twice, so the first match is the only one.
func withoutKey(rows []*objectRow, key string) []*objectRow {
	for i, entry := range rows {
		if entry.full == key {
			return append(rows[:i:i], rows[i+1:]...)
		}
	}
	return rows
}

// objectRow is one displayed entry of a listing: either a folder or an object. Rows are held by
// pointer, so an aggregate scanned into one survives the re-render that filtering performs.
type objectRow struct {
	name   string // display name; a folder keeps its trailing delimiter
	full   string // full key, or full prefix for a folder
	folder bool
	statCells
}

// objectRows renders a listing as display rows, folders first, then objects. Both come from
// S3 already sorted by key.
//
// A folder starts out with unknown aggregate cells: S3 says nothing about what a prefix holds
// until <i> walks it. An object fills them from its own metadata, except for the key count, which
// only means something for a folder.
func objectRows(listing *s3.Listing) []*objectRow {
	rows := make([]*objectRow, 0, listing.Total())

	for _, prefix := range listing.Prefixes {
		rows = append(rows, &objectRow{
			name:      s3.Name(listing.Prefix, prefix),
			full:      prefix,
			folder:    true,
			statCells: unknownCells(),
		})
	}

	for _, object := range listing.Objects {
		storageClass := object.StorageClass
		if storageClass == "" {
			storageClass = unknownCell
		}
		rows = append(rows, &objectRow{
			name: s3.Name(listing.Prefix, object.Key),
			full: object.Key,
			statCells: statCells{
				objects:      unknownCell,
				size:         util.FormatBytes(object.Size),
				lastModified: s3.FormatTime(object.LastModified),
				storageClass: storageClass,
			},
		})
	}

	return rows
}

// objectStatsColumn is the first aggregate column of the objects table.
const objectStatsColumn = 1

// fillObjectsTable rebuilds the table rows, headers included, and puts the cursor on the
// first entry.
func fillObjectsTable(table *tview.Table, rows []*objectRow, labelColor tcell.Color) {
	table.Clear()
	util.SetTableHeaders(
		table,
		labelColor,
		"Name",
		"Objects",
		"Size",
		"Last-modified",
		"Storage-class",
	)

	for i, entry := range rows {
		row := i + 1
		table.
			SetCell(row, 0, tview.NewTableCell(entry.name)).
			SetCell(row, 1, tview.NewTableCell(entry.objects)).
			SetCell(row, 2, tview.NewTableCell(entry.size)).
			SetCell(row, 3, tview.NewTableCell(entry.lastModified)).
			SetCell(row, 4, tview.NewTableCell(entry.storageClass))
	}

	if table.GetRowCount() > 1 {
		table.Select(1, 0)
	}
}

// filterObjectRows keeps the rows whose name fuzzy-matches the filter, in match order.
func filterObjectRows(rows []*objectRow, filter string) []*objectRow {
	if filter == "" {
		return rows
	}

	names := make([]string, len(rows))
	for i, entry := range rows {
		names[i] = entry.name
	}

	matches := fuzzy.Find(filter, names)
	filtered := make([]*objectRow, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, rows[match.Index])
	}
	return filtered
}
