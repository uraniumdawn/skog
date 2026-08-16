// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/gdamore/tcell/v2"

	"github.com/uraniumdawn/skog/pkg/iceberg"
	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// IcebergSnapshots opens the snapshots one metadata document lists, newest first.
func (app *App) IcebergSnapshots(target IcebergVersionTarget) {
	pageKey := app.icebergSnapshotsPageKey(target)
	display := s3.DisplayPath(target.Bucket, target.Prefix)

	fetch(app, "reading the snapshots of "+path.Base(target.Key),
		func(ctx context.Context, client *s3.Client) (*iceberg.Metadata, error) {
			return iceberg.LoadMetadata(ctx, iceberg.From(client), target.Bucket, target.Key)
		},
		func(metadata *iceberg.Metadata) {
			app.showIcebergSnapshots(pageKey, display, target, metadata)
		},
	)
}

func (app *App) showIcebergSnapshots(
	pageKey, display string,
	target IcebergVersionTarget,
	metadata *iceberg.Metadata,
) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)

	snapshots := append([]iceberg.Snapshot(nil), metadata.Snapshots...)
	sort.SliceStable(snapshots, func(i, j int) bool {
		return snapshots[i].TimestampMS > snapshots[j].TimestampMS
	})

	// A table can hold thousands of snapshots, so the page fills a batch at a time the way the
	// viewer does, and <n> adds another.
	batch := app.Config.ViewerPageRows()
	shown := min(batch, len(snapshots))
	visible := snapshots[:shown]

	table := app.newIcebergTable()
	what := fmt.Sprintf("%s@%s snapshots", display, path.Base(target.Key))

	render := func(filter string) {
		cells, index := filterIcebergRows(icebergSnapshotCells(metadata, snapshots[:shown]), filter)
		visible = make([]iceberg.Snapshot, 0, len(index))
		for _, i := range index {
			visible = append(visible, snapshots[i])
		}
		fillIcebergTable(table, []string{
			"Snapshot-id", "Time", "Operation",
			"Added-files", "Deleted-files", "Added-records", "Total-records", "Refs",
		}, cells, labelColor)
		util.SetSearchableTitle(table, icebergTitle(what, shown, len(snapshots)), filter)
	}
	render("")

	selected := func() (iceberg.Snapshot, bool) {
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return iceberg.Snapshot{}, false
		}
		return visible[row-1], true
	}

	// open descends into the manifests of the snapshot under the cursor.
	open := func() {
		snapshot, ok := selected()
		if !ok {
			return
		}
		Publish(IcebergChannel, GetIcebergManifestsEventType, Payload{
			IcebergSnapshotTarget{
				Bucket:     target.Bucket,
				Prefix:     target.Prefix,
				Key:        target.Key,
				SnapshotID: snapshot.SnapshotID,
			},
			false,
		})
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			Publish(IcebergChannel, GetIcebergSnapshotsEventType, Payload{target, true})
			return nil
		}

		if IsKey(event, 'n') {
			// <n> is off the menu once every snapshot is shown, so this only guards a stale
			// keypress.
			if shown >= len(snapshots) {
				return nil
			}
			shown = min(shown+batch, len(snapshots))
			render(app.CurrentFilters[pageKey])
			app.SetPageMenu(pageKey, icebergSnapshotsMenu(shown, len(snapshots)))
			return nil
		}

		return event
	})

	app.AddToPagesRegistry(pageKey, table, icebergSnapshotsMenu(shown, len(snapshots)), true)
	// Above the snapshots is the document that lists them, below one of them its manifests.
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, app.leaveVersion(target), open)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})
}

// icebergSnapshotsMenu is the menu of the snapshots page, offering <n> only while snapshots are
// left to show.
func icebergSnapshotsMenu(shown, total int) string {
	if shown >= total {
		return IcebergSnapshotsPageMenu
	}
	return IcebergSnapshotsBatchedPageMenu
}

// icebergSnapshotCells renders the snapshots as the cells the table shows.
//
// The counts come from each snapshot's own summary, which is where a writer records what its
// commit did. A writer that recorded nothing leaves them blank rather than showing a zero it
// never claimed.
func icebergSnapshotCells(metadata *iceberg.Metadata, snapshots []iceberg.Snapshot) [][]string {
	cells := make([][]string, 0, len(snapshots))
	for _, snapshot := range snapshots {
		refs := metadata.RefsAt(snapshot.SnapshotID)
		if metadata.IsCurrent(snapshot.SnapshotID) {
			refs = append([]string{"current"}, refs...)
		}

		cells = append(cells, []string{
			strconv.FormatInt(snapshot.SnapshotID, 10),
			s3.FormatTime(snapshot.Committed()),
			snapshot.Operation(),
			summaryCount(snapshot, "added-data-files"),
			summaryCount(snapshot, "deleted-data-files"),
			summaryCount(snapshot, "added-records"),
			summaryCount(snapshot, "total-records"),
			strings.Join(refs, ", "),
		})
	}
	return cells
}

// summaryCount is one figure out of a snapshot's summary, grouped for reading, empty for a
// figure the writer did not record.
func summaryCount(snapshot iceberg.Snapshot, key string) string {
	value, ok := snapshot.Summary[key]
	if !ok {
		return ""
	}
	number, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return value
	}
	return util.FormatNumber(number)
}

// IcebergManifests opens the manifests of one snapshot.
func (app *App) IcebergManifests(target IcebergSnapshotTarget) {
	pageKey := app.icebergManifestsPageKey(target)
	display := s3.DisplayPath(target.Bucket, target.Prefix)

	fetch(app, fmt.Sprintf("reading the manifests of snapshot %d", target.SnapshotID),
		func(ctx context.Context, client *s3.Client) (*icebergManifests, error) {
			return loadManifests(ctx, iceberg.From(client), target)
		},
		func(found *icebergManifests) {
			app.showIcebergManifests(pageKey, display, target, found)
		},
	)
}

// icebergManifests is a snapshot's manifests and the table location their paths are relative to.
type icebergManifests struct {
	files []iceberg.ManifestFile
	// location is the table's own, which is what a path recorded relative to the table is
	// resolved against.
	location string
	// listed reports that the manifests came from a manifest list, and so carry counts. A
	// version 1 snapshot may name its manifests outright, and then there are none to show.
	listed bool
}

// loadManifests reads a snapshot's manifests, by whichever of the two ways it records them.
func loadManifests(
	ctx context.Context,
	source iceberg.Source,
	target IcebergSnapshotTarget,
) (*icebergManifests, error) {
	metadata, err := iceberg.LoadMetadata(ctx, source, target.Bucket, target.Key)
	if err != nil {
		return nil, err
	}

	snapshot := metadata.SnapshotByID(target.SnapshotID)
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot %d is not in this metadata document", target.SnapshotID)
	}

	found := &icebergManifests{location: metadata.Location}

	// A version 1 snapshot may list its manifests outright instead of pointing at a manifest
	// list. There are no counts to go with them then, only the paths.
	if snapshot.ManifestList == "" {
		for _, path := range snapshot.Manifests {
			found.files = append(found.files, iceberg.ManifestFile{Path: path})
		}
		return found, nil
	}

	bucket, key, err := iceberg.ParseLocation(snapshot.ManifestList, metadata.Location)
	if err != nil {
		return nil, err
	}

	found.files, err = iceberg.LoadManifestList(ctx, source, bucket, key)
	if err != nil {
		return nil, err
	}
	found.listed = true
	return found, nil
}

func (app *App) showIcebergManifests(
	pageKey, display string,
	target IcebergSnapshotTarget,
	found *icebergManifests,
) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	files := found.files
	visible := files

	table := app.newIcebergTable()
	what := fmt.Sprintf("%s#%d manifests", display, target.SnapshotID)
	// A snapshot that named its manifests outright has no manifest list, and so no counts to
	// show; empty count columns would otherwise read as zeros.
	if !found.listed && len(files) > 0 {
		what += " (no manifest list, so no counts)"
	}

	render := func(filter string) {
		cells, index := filterIcebergRows(icebergManifestCells(files), filter)
		visible = make([]iceberg.ManifestFile, 0, len(index))
		for _, i := range index {
			visible = append(visible, files[i])
		}
		fillIcebergTable(table, []string{
			"Path", "Content", "Added", "Existing", "Deleted", "Rows", "Length",
		}, cells, labelColor)
		util.SetSearchableTitle(table, icebergTitle(what, len(files), len(files)), filter)
	}
	render("")

	// open descends into the files the manifest under the cursor lists. Its path is resolved
	// here, where the table's location is at hand.
	open := func() {
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return
		}
		manifest := visible[row-1]

		bucket, key, err := iceberg.ParseLocation(manifest.Path, found.location)
		if err != nil {
			SendStatusWithDefaultTTL("[red]" + err.Error())
			return
		}

		Publish(IcebergChannel, GetIcebergFilesEventType, Payload{
			IcebergManifestTarget{
				Bucket:         target.Bucket,
				Prefix:         target.Prefix,
				Key:            target.Key,
				SnapshotID:     target.SnapshotID,
				ManifestBucket: bucket,
				ManifestKey:    key,
				Sequence:       manifest.SequenceNumber,
			},
			false,
		})
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			Publish(IcebergChannel, GetIcebergManifestsEventType, Payload{target, true})
			return nil
		}
		return event
	})

	app.AddToPagesRegistry(pageKey, table, IcebergManifestsPageMenu, true)
	// Above the manifests are the snapshots they belong to, below one of them its files.
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, func() {
		Publish(IcebergChannel, GetIcebergSnapshotsEventType, Payload{
			IcebergVersionTarget{Bucket: target.Bucket, Prefix: target.Prefix, Key: target.Key},
			false,
		})
	}, open)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})
}

// icebergManifestCells renders the manifests as the cells the table shows.
func icebergManifestCells(files []iceberg.ManifestFile) [][]string {
	cells := make([][]string, 0, len(files))
	for _, file := range files {
		cells = append(cells, []string{
			path.Base(file.Path),
			iceberg.ContentName(file.Content),
			util.FormatNumber(file.AddedFiles),
			util.FormatNumber(file.ExistingFiles),
			util.FormatNumber(file.DeletedFiles),
			util.FormatNumber(file.AddedRows + file.ExistingRows),
			util.FormatBytes(file.Length),
		})
	}
	return cells
}

// IcebergFiles opens the data and delete files one manifest lists.
func (app *App) IcebergFiles(target IcebergManifestTarget) {
	pageKey := app.icebergFilesPageKey(target)

	fetch(app, "reading "+path.Base(target.ManifestKey),
		func(ctx context.Context, client *s3.Client) (*iceberg.Manifest, error) {
			return iceberg.LoadManifest(
				ctx,
				iceberg.From(client),
				target.ManifestBucket,
				target.ManifestKey,
				target.Sequence,
			)
		},
		func(manifest *iceberg.Manifest) {
			app.showIcebergFiles(pageKey, target, manifest)
		},
	)
}

func (app *App) showIcebergFiles(
	pageKey string,
	target IcebergManifestTarget,
	manifest *iceberg.Manifest,
) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	entries := manifest.Entries

	batch := app.Config.ViewerPageRows()
	shown := min(batch, len(entries))
	visible := entries[:shown]

	// A path recorded relative to the table is resolved against the table, which is the prefix
	// this page was reached through.
	base := "s3://" + target.Bucket + s3.Delimiter + target.Prefix

	table := app.newIcebergTable()
	what := fmt.Sprintf(
		"%s files", s3.DisplayPath(target.ManifestBucket, target.ManifestKey),
	)
	if manifest.Truncated {
		what += " (partial)"
	}

	render := func(filter string) {
		cells, index := filterIcebergRows(
			icebergFileCells(manifest.Spec, entries[:shown]), filter,
		)
		visible = make([]iceberg.ManifestEntry, 0, len(index))
		for _, i := range index {
			visible = append(visible, entries[i])
		}
		fillIcebergTable(table, []string{
			"Path", "Content", "Status", "Records", "Size", "Partition",
		}, cells, labelColor)
		util.SetSearchableTitle(table, icebergTitle(what, shown, len(entries)), filter)
	}
	render("")

	// selectedFile locates the file the cursor is on. A file is an ordinary object once it has
	// been located, which is what lets the viewer and the download read it.
	selectedFile := func() (string, string, bool) {
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return "", "", false
		}

		bucket, key, err := iceberg.ParseLocation(visible[row-1].DataFile.Path, base)
		if err != nil {
			SendStatusWithDefaultTTL("[red]" + err.Error())
			return "", "", false
		}
		return bucket, key, true
	}

	// open reads the file the cursor is on. It is the bottom of the chain: what is under a data
	// file is its rows, and the viewer is where they are shown.
	open := func() {
		bucket, key, ok := selectedFile()
		if !ok {
			return
		}
		// The origin is this page, so <h> out of the viewer comes back to the manifest rather
		// than to the object's metadata, which belongs to the other hierarchy.
		app.ViewData(ObjectDataTarget{Bucket: bucket, Key: key, Origin: pageKey})
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			Publish(IcebergChannel, GetIcebergFilesEventType, Payload{target, true})
			return nil
		}

		if IsKey(event, 'd') {
			if bucket, key, ok := selectedFile(); ok {
				app.Download(bucket, key, false)
			}
			return nil
		}

		if IsKey(event, 'n') {
			// <n> is off the menu once every file is shown, so this only guards a stale keypress.
			if shown >= len(entries) {
				return nil
			}
			shown = min(shown+batch, len(entries))
			render(app.CurrentFilters[pageKey])
			app.SetPageMenu(pageKey, icebergFilesMenu(shown, len(entries)))
			return nil
		}

		return event
	})

	app.AddToPagesRegistry(pageKey, table, icebergFilesMenu(shown, len(entries)), true)
	// Above the files is the manifest listing them, below one of them its rows.
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, func() {
		Publish(IcebergChannel, GetIcebergManifestsEventType, Payload{target.snapshot(), false})
	}, open)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})
}

// icebergFilesMenu is the menu of the files page, offering <n> only while files are left to
// show.
func icebergFilesMenu(shown, total int) string {
	if shown >= total {
		return IcebergFilesPageMenu
	}
	return IcebergFilesBatchedPageMenu
}

// icebergFileCells renders the manifest's entries as the cells the table shows, with their
// partition values read under the spec the manifest itself declares.
func icebergFileCells(spec []iceberg.PartitionField, entries []iceberg.ManifestEntry) [][]string {
	cells := make([][]string, 0, len(entries))
	for _, entry := range entries {
		cells = append(cells, []string{
			path.Base(entry.DataFile.Path),
			iceberg.ContentName(entry.DataFile.Content),
			iceberg.StatusName(entry.Status),
			util.FormatNumber(entry.DataFile.RecordCount),
			util.FormatBytes(entry.DataFile.SizeBytes),
			iceberg.FormatPartition(spec, entry.DataFile.Partition),
		})
	}
	return cells
}
