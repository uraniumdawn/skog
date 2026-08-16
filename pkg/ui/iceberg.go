// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"fmt"
	"path"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"github.com/sahilm/fuzzy"

	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// The Iceberg resource is a hierarchy of its own over the same bucket the S3 resource shows:
// buckets, the levels of a warehouse, a table, its metadata documents, the snapshots of one, the
// manifests of a snapshot and the files of a manifest. A row here is never a plain object, and
// nothing on these pages modifies anything — a data file removed from under a table breaks it in
// a way Iceberg cannot repair.
const (
	// GetIcebergBucketsEventType opens the bucket list of the Iceberg resource.
	GetIcebergBucketsEventType EventType = "iceberg:buckets"
	// GetIcebergLevelEventType opens one level of a warehouse: its namespaces and its tables.
	GetIcebergLevelEventType EventType = "iceberg:level"
	// GetIcebergTableEventType opens a table's metadata documents.
	GetIcebergTableEventType EventType = "iceberg:table"
	// GetIcebergOverviewEventType opens what one metadata document says about the table.
	GetIcebergOverviewEventType EventType = "iceberg:overview"
	// GetIcebergSchemaEventType opens the schema one metadata document declares.
	GetIcebergSchemaEventType EventType = "iceberg:schema"
	// GetIcebergSnapshotsEventType opens the snapshots one metadata document lists.
	GetIcebergSnapshotsEventType EventType = "iceberg:snapshots"
	// GetIcebergManifestsEventType opens the manifests of one snapshot.
	GetIcebergManifestsEventType EventType = "iceberg:manifests"
	// GetIcebergFilesEventType opens the files of one manifest.
	GetIcebergFilesEventType EventType = "iceberg:files"
)

// IcebergChannel carries every Iceberg navigation event.
var IcebergChannel = make(chan Event)

// IcebergLevelTarget identifies one level of a warehouse. Prefix is empty for the root of the
// bucket and otherwise ends with a delimiter.
type IcebergLevelTarget struct {
	Bucket string
	Prefix string
}

// IcebergTableTarget identifies a table by the level that holds it. Prefix ends with a
// delimiter, and the table's documents are under Prefix + iceberg.MetadataDir.
type IcebergTableTarget struct {
	Bucket string
	Prefix string
}

// IcebergVersionTarget identifies one metadata document of a table. Prefix is the table, Key the
// document itself.
type IcebergVersionTarget struct {
	Bucket string
	Prefix string
	Key    string
}

// IcebergSnapshotTarget identifies one snapshot of one metadata document.
type IcebergSnapshotTarget struct {
	Bucket string
	Prefix string
	Key    string
	// SnapshotID is the snapshot's own id, which is what the metadata document lists it under.
	SnapshotID int64
}

// IcebergManifestTarget identifies one manifest of one snapshot.
type IcebergManifestTarget struct {
	Bucket string
	Prefix string
	Key    string
	// SnapshotID names the snapshot the manifest was reached through, which is what <h> goes
	// back to.
	SnapshotID int64
	// ManifestBucket and ManifestKey locate the manifest itself. They are resolved against the
	// table's location where it is opened from, since a manifest list may record a path relative
	// to the table or absolute, and may name a bucket other than the one being browsed.
	ManifestBucket string
	ManifestKey    string
	// Sequence is the manifest's sequence number, which an entry carrying none inherits.
	Sequence int64
}

// RunIcebergEventHandler processes Iceberg navigation events from the channel.
//
// Every event resolves to a page, the way an S3 event does: an already opened page is switched
// to unless the event forces a refresh.
func (app *App) RunIcebergEventHandler(ctx context.Context, in chan Event) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("shutting down iceberg event handler")
				return
			case event := <-in:
				app.handleIcebergEvent(event)
			}
		}
	}()
}

// handleIcebergEvent opens the page one event names.
func (app *App) handleIcebergEvent(event Event) {
	switch event.Type {
	case GetIcebergBucketsEventType:
		app.openPage(app.icebergBucketsPageKey(), event.Payload.Force, app.IcebergBuckets)

	case GetIcebergLevelEventType:
		if target, ok := icebergTarget[IcebergLevelTarget](event); ok {
			app.openPage(app.icebergLevelPageKey(target), event.Payload.Force, func() {
				app.IcebergLevel(target)
			})
		}

	case GetIcebergTableEventType:
		if target, ok := icebergTarget[IcebergTableTarget](event); ok {
			app.openPage(app.icebergTablePageKey(target), event.Payload.Force, func() {
				app.IcebergTable(target)
			})
		}

	case GetIcebergOverviewEventType:
		if target, ok := icebergTarget[IcebergVersionTarget](event); ok {
			app.openPage(app.icebergOverviewPageKey(target), event.Payload.Force, func() {
				app.IcebergOverview(target)
			})
		}

	case GetIcebergSchemaEventType:
		if target, ok := icebergTarget[IcebergVersionTarget](event); ok {
			app.openPage(app.icebergSchemaPageKey(target), event.Payload.Force, func() {
				app.IcebergSchema(target)
			})
		}

	case GetIcebergSnapshotsEventType:
		if target, ok := icebergTarget[IcebergVersionTarget](event); ok {
			app.openPage(app.icebergSnapshotsPageKey(target), event.Payload.Force, func() {
				app.IcebergSnapshots(target)
			})
		}

	case GetIcebergManifestsEventType:
		if target, ok := icebergTarget[IcebergSnapshotTarget](event); ok {
			app.openPage(app.icebergManifestsPageKey(target), event.Payload.Force, func() {
				app.IcebergManifests(target)
			})
		}

	case GetIcebergFilesEventType:
		if target, ok := icebergTarget[IcebergManifestTarget](event); ok {
			app.openPage(app.icebergFilesPageKey(target), event.Payload.Force, func() {
				app.IcebergFiles(target)
			})
		}
	}
}

// icebergTarget takes the target out of an event, logging an event that carries the wrong one
// rather than acting on it.
func icebergTarget[T any](event Event) (T, bool) {
	target, ok := event.Payload.Data.(T)
	if !ok {
		log.Error().Str("event", string(event.Type)).Msg("iceberg event without its target")
	}
	return target, ok
}

// Page keys. They are built from the profile, the bucket and the keys themselves, so the same
// table under two profiles is two pages. S3 keys are case-sensitive, so nothing is lower-cased.

func (app *App) icebergBucketsPageKey() string {
	return app.SelectedProfileName() + ":iceberg:buckets"
}

func (app *App) icebergLevelPageKey(target IcebergLevelTarget) string {
	return app.icebergPrefix(target.Bucket, target.Prefix)
}

func (app *App) icebergTablePageKey(target IcebergTableTarget) string {
	return app.icebergPrefix(target.Bucket, target.Prefix) + ":table"
}

func (app *App) icebergOverviewPageKey(target IcebergVersionTarget) string {
	return app.icebergVersion(target) + ":info"
}

func (app *App) icebergSchemaPageKey(target IcebergVersionTarget) string {
	return app.icebergVersion(target) + ":schema"
}

func (app *App) icebergSnapshotsPageKey(target IcebergVersionTarget) string {
	return app.icebergVersion(target) + ":snapshots"
}

func (app *App) icebergManifestsPageKey(target IcebergSnapshotTarget) string {
	return app.icebergVersion(target.version()) +
		"#" + strconv.FormatInt(target.SnapshotID, 10) + ":manifests"
}

func (app *App) icebergFilesPageKey(target IcebergManifestTarget) string {
	return app.icebergVersion(target.version()) +
		"#" + strconv.FormatInt(target.SnapshotID, 10) +
		"!" + s3.DisplayPath(target.ManifestBucket, target.ManifestKey) + ":files"
}

// icebergPrefix is the head every Iceberg page key shares: the profile, the resource and the
// level in the bucket.
func (app *App) icebergPrefix(bucket, prefix string) string {
	return app.SelectedProfileName() + ":iceberg:" + s3.DisplayPath(bucket, prefix)
}

// icebergVersion is the head of every page key belonging to one metadata document.
func (app *App) icebergVersion(target IcebergVersionTarget) string {
	return app.icebergPrefix(target.Bucket, target.Prefix) + "@" + path.Base(target.Key)
}

// version is the metadata document a snapshot was reached through.
func (t IcebergSnapshotTarget) version() IcebergVersionTarget {
	return IcebergVersionTarget{Bucket: t.Bucket, Prefix: t.Prefix, Key: t.Key}
}

// version is the metadata document a manifest was reached through.
func (t IcebergManifestTarget) version() IcebergVersionTarget {
	return IcebergVersionTarget{Bucket: t.Bucket, Prefix: t.Prefix, Key: t.Key}
}

// snapshot is the snapshot a manifest was reached through.
func (t IcebergManifestTarget) snapshot() IcebergSnapshotTarget {
	return IcebergSnapshotTarget{
		Bucket:     t.Bucket,
		Prefix:     t.Prefix,
		Key:        t.Key,
		SnapshotID: t.SnapshotID,
	}
}

// tableName is what a table is called on screen: the last level of its prefix, which is the
// name the catalog would know it by.
func tableName(prefix string) string {
	trimmed := strings.TrimSuffix(prefix, s3.Delimiter)
	if trimmed == "" {
		return s3.Delimiter
	}
	return path.Base(trimmed)
}

// newIcebergTable builds the table every Iceberg listing page is drawn in.
func (app *App) newIcebergTable() *tview.Table {
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
	return table
}

// fillIcebergTable rebuilds a table from cells already rendered as text, and puts the cursor on
// the first row.
func fillIcebergTable(
	table *tview.Table,
	headers []string,
	rows [][]string,
	labelColor tcell.Color,
) {
	table.Clear()
	util.SetTableHeaders(table, labelColor, headers...)

	for i, cells := range rows {
		for j, cell := range cells {
			table.SetCell(i+1, j, tview.NewTableCell(cell))
		}
	}

	if table.GetRowCount() > 1 {
		table.Select(1, 0)
	}
}

// filterIcebergRows keeps the rows fuzzy-matching the filter, in match order, and reports which
// row of the unfiltered page each kept row came from — that index is how a keypress finds what
// the row stands for.
//
// A row matches on any of its cells: which column holds what is being looked for is exactly what
// someone reading a manifest for the first time does not know.
func filterIcebergRows(rows [][]string, filter string) ([][]string, []int) {
	index := make([]int, len(rows))
	for i := range rows {
		index[i] = i
	}
	if filter == "" {
		return rows, index
	}

	haystack := make([]string, len(rows))
	for i, cells := range rows {
		haystack[i] = strings.Join(cells, " ")
	}

	matches := fuzzy.Find(filter, haystack)
	filtered := make([][]string, 0, len(matches))
	kept := make([]int, 0, len(matches))
	for _, match := range matches {
		filtered = append(filtered, rows[match.Index])
		kept = append(kept, match.Index)
	}
	return filtered, kept
}

// icebergTitle names a page and how many rows of it are shown. shown is less than total on a
// page that loads in batches.
func icebergTitle(what string, shown, total int) string {
	if shown < total {
		return fmt.Sprintf(" %s [%d/%d] ", what, shown, total)
	}
	return fmt.Sprintf(" %s [%d] ", what, total)
}
