// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"errors"
	"sort"

	"github.com/gdamore/tcell/v2"
	"github.com/rs/zerolog/log"

	"github.com/uraniumdawn/skog/pkg/iceberg"
	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// IcebergBuckets opens the bucket list of the Iceberg resource. The buckets are the same ones
// the S3 resource shows; what differs is what a row leads to.
func (app *App) IcebergBuckets() {
	pageKey := app.icebergBucketsPageKey()
	profile := app.SelectedProfileName()

	fetch(app, "listing buckets",
		func(ctx context.Context, client *s3.Client) ([]s3.Bucket, error) {
			return client.ListBuckets(ctx)
		},
		func(buckets []s3.Bucket) {
			app.showBuckets(pageKey, profile, buckets, bucketsNav{
				label: "iceberg",
				menu:  IcebergBucketsPageMenu,
				open: func(bucket string) {
					Publish(
						IcebergChannel,
						GetIcebergLevelEventType,
						Payload{IcebergLevelTarget{Bucket: bucket}, false},
					)
				},
				refresh: func() {
					Publish(IcebergChannel, GetIcebergBucketsEventType, Payload{nil, true})
				},
			})
		},
	)
}

// What a row of a level turned out to be.
const (
	kindUnknown   = "?"
	kindNamespace = "namespace"
	kindTable     = "table"
)

// icebergLevelRow is one entry of a level: a prefix that is either a namespace to walk into or a
// table to open. Rows are held by pointer so that what a probe learned about one survives the
// re-render that filtering performs.
type icebergLevelRow struct {
	name   string
	prefix string
	kind   string
}

// IcebergLevel opens one level of a warehouse: the prefixes under it, each a namespace or a
// table.
//
// Only prefixes are listed. A warehouse level holds tables, and the objects that happen to sit
// beside them are what the S3 resource is for.
func (app *App) IcebergLevel(target IcebergLevelTarget) {
	pageKey := app.icebergLevelPageKey(target)
	path := s3.DisplayPath(target.Bucket, target.Prefix)

	fetch(app, "listing "+path,
		func(ctx context.Context, client *s3.Client) (*s3.Listing, error) {
			return client.ListObjects(ctx, target.Bucket, target.Prefix)
		},
		func(listing *s3.Listing) {
			app.showIcebergLevel(pageKey, path, target, listing)
		},
	)
}

func (app *App) showIcebergLevel(
	pageKey, path string,
	target IcebergLevelTarget,
	listing *s3.Listing,
) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	rows := icebergLevelRows(listing)
	visible := rows
	// token continues the level past the batch on screen, and is empty once it is all loaded.
	token := listing.NextToken

	table := app.newIcebergTable()

	// render redraws the table for the filter in force, which is also what a new batch needs.
	render := func(filter string) {
		cells, index := filterIcebergRows(icebergLevelCells(rows), filter)
		visible = make([]*icebergLevelRow, 0, len(index))
		for _, i := range index {
			visible = append(visible, rows[i])
		}
		fillIcebergTable(table, []string{"Name", "Type"}, cells, labelColor)
		util.SetSearchableTitle(table, icebergTitle(path, len(rows), len(rows)), filter)
	}
	render("")

	// renderKind writes what a probe learned into the row it belongs to, if that row is still on
	// screen: a probe that finishes after the page was filtered finds its row gone, and the value
	// stays on the row itself for the next render to pick up.
	renderKind := func(entry *icebergLevelRow) {
		for i, candidate := range visible {
			if candidate != entry {
				continue
			}
			if cell := table.GetCell(i+1, 1); cell != nil {
				cell.SetText(entry.kind)
			}
			return
		}
	}

	selected := func() *icebergLevelRow {
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return nil
		}
		return visible[row-1]
	}

	// openRow descends into what a row turned out to be: a namespace lists the level under it, a
	// table opens its metadata documents.
	openRow := func(entry *icebergLevelRow) {
		if entry.kind == kindTable {
			Publish(
				IcebergChannel,
				GetIcebergTableEventType,
				Payload{IcebergTableTarget{Bucket: target.Bucket, Prefix: entry.prefix}, false},
			)
			return
		}
		Publish(
			IcebergChannel,
			GetIcebergLevelEventType,
			Payload{IcebergLevelTarget{Bucket: target.Bucket, Prefix: entry.prefix}, false},
		)
	}

	// open descends into the row the cursor is on. A row the probe has not reached yet is probed
	// here, so a cancelled probe costs the column and nothing else.
	open := func() {
		entry := selected()
		if entry == nil {
			return
		}
		// Anything but a settled answer is asked again here, the row being probed at this moment
		// included: what it is is not known yet, and guessing would open the wrong page.
		if entry.kind == kindTable || entry.kind == kindNamespace {
			openRow(entry)
			return
		}

		app.probeOne(target.Bucket, entry, func() {
			renderKind(entry)
			openRow(entry)
		})
	}

	// up opens the level above this one, or the bucket list when this is the root of a bucket.
	up := func() {
		parent, ok := s3.Parent(target.Prefix)
		if !ok {
			Publish(IcebergChannel, GetIcebergBucketsEventType, Payload{nil, false})
			return
		}
		Publish(
			IcebergChannel,
			GetIcebergLevelEventType,
			Payload{IcebergLevelTarget{Bucket: target.Bucket, Prefix: parent}, false},
		)
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			// The refreshed page starts with nothing probed, so a probe filling in the rows of
			// the page being replaced has nothing left to fill.
			app.CancelJob()
			Publish(IcebergChannel, GetIcebergLevelEventType, Payload{target, true})
			return nil
		}

		if event.Key() == tcell.KeyEsc {
			if app.CancelJob() {
				return nil
			}
			return event
		}

		if IsKey(event, 'n') {
			// <n> is off the menu once the level is complete, so this only guards a stale
			// keypress.
			if token == "" {
				return nil
			}

			fetch(app, "listing more of "+path,
				func(ctx context.Context, client *s3.Client) (*s3.Listing, error) {
					return client.ListMore(ctx, target.Bucket, target.Prefix, token)
				},
				func(next *s3.Listing) {
					rows = appendLevelRows(rows, next)
					token = next.NextToken
					render(app.CurrentFilters[pageKey])
					app.SetPageMenu(pageKey, icebergLevelMenu(token))
					app.probeLevel(target.Bucket, rows, renderKind)
				},
			)
			return nil
		}

		return event
	})

	app.AddToPagesRegistry(pageKey, table, icebergLevelMenu(token), true)
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, up, open)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})

	// Nothing on a listing says which prefixes are tables, so the column is filled in behind the
	// page rather than kept off it.
	app.probeLevel(target.Bucket, rows, renderKind)
}

// icebergLevelMenu is the menu of a level: the one offering <n> only while a next batch exists.
func icebergLevelMenu(token string) string {
	if token == "" {
		return IcebergLevelPageMenu
	}
	return IcebergLevelBatchedPageMenu
}

// icebergLevelRows renders the prefixes of a listing as display rows, none of them yet known to
// be a table or not.
func icebergLevelRows(listing *s3.Listing) []*icebergLevelRow {
	rows := make([]*icebergLevelRow, 0, len(listing.Prefixes))
	for _, prefix := range listing.Prefixes {
		rows = append(rows, &icebergLevelRow{
			name:   s3.Name(listing.Prefix, prefix),
			prefix: prefix,
			kind:   kindUnknown,
		})
	}
	return rows
}

// appendLevelRows adds a batch to the rows already on the page, in key order, keeping what has
// been probed about the rows that were already there.
func appendLevelRows(rows []*icebergLevelRow, batch *s3.Listing) []*icebergLevelRow {
	rows = append(rows, icebergLevelRows(batch)...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].prefix < rows[j].prefix })
	return rows
}

// icebergLevelCells renders the rows as the cells the table shows.
func icebergLevelCells(rows []*icebergLevelRow) [][]string {
	cells := make([][]string, 0, len(rows))
	for _, entry := range rows {
		cells = append(cells, []string{entry.name, entry.kind})
	}
	return cells
}

// probeLevel fills in the Type column of a level, a row at a time.
//
// Whether a prefix is a table is one listing per prefix, which is why it is not part of building
// the page: a level of fifty namespaces would be fifty requests before anything was shown.
//
// It holds the job slot for as long as it runs, so <Esc> stops it, and it claims that slot only
// if it is free — nobody asked for this walk, so it does not announce itself and does not
// complain when something else is running. Its progress is the column filling in; a row it did
// not reach keeps its question mark and is probed when it is opened.
func (app *App) probeLevel(bucket string, rows []*icebergLevelRow, done func(*icebergLevelRow)) {
	pending := make([]*icebergLevelRow, 0, len(rows))
	for _, entry := range rows {
		if entry.kind == kindUnknown {
			pending = append(pending, entry)
		}
	}
	if len(pending) == 0 || !app.tryBeginJob("a probe") {
		return
	}

	ctx, cancel := context.WithCancel(app.ctx)
	app.setJobCancel(cancel)

	go func() {
		defer cancel()
		defer app.endJob()

		client, err := app.S3Client(ctx)
		if err != nil {
			failed("looking for tables", err)
			return
		}
		source := iceberg.From(client)

		// setKind writes what is now known about a row, on the UI goroutine, where the rows and
		// the table both belong.
		setKind := func(row *icebergLevelRow, kind string) {
			app.QueueUpdateDraw(func() {
				row.kind = kind
				done(row)
			})
		}

		for _, entry := range pending {
			setKind(entry, scanningCell)

			kind, err := probeKind(ctx, source, bucket, entry.prefix)
			switch {
			case errors.Is(err, context.Canceled) || ctx.Err() != nil:
				setKind(entry, kindUnknown)
				SendStatusWithDefaultTTL("the search for tables was cancelled")
				return
			case err != nil:
				// One prefix that cannot be listed — a level the profile may not read — leaves
				// its own row unknown and does not end the walk over the rest.
				log.Warn().Err(err).Str("prefix", entry.prefix).Msg("could not probe a prefix")
				setKind(entry, kindUnknown)
				continue
			}

			setKind(entry, kind)
		}
	}()
}

// probeOne settles what a single row is, for a row opened before the level's probe reached it.
func (app *App) probeOne(bucket string, entry *icebergLevelRow, done func()) {
	fetch(app, "checking "+s3.DisplayPath(bucket, entry.prefix),
		func(ctx context.Context, client *s3.Client) (string, error) {
			return probeKind(ctx, iceberg.From(client), bucket, entry.prefix)
		},
		func(kind string) {
			entry.kind = kind
			done()
		},
	)
}

// probeKind is what a prefix turned out to be.
func probeKind(
	ctx context.Context,
	source iceberg.Source,
	bucket, prefix string,
) (string, error) {
	isTable, err := iceberg.IsTable(ctx, source, bucket, prefix)
	if err != nil {
		return kindUnknown, err
	}
	if isTable {
		return kindTable, nil
	}
	return kindNamespace, nil
}
