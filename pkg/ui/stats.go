// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"errors"
	"fmt"

	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// statCells are the aggregate columns of a row: unknown until the row is scanned, since S3
// reports nothing about what a prefix contains.
type statCells struct {
	objects      string
	size         string
	lastModified string
	storageClass string
}

const (
	// unknownCell marks a column that has not been scanned.
	unknownCell = "-"
	// scanningCell marks a column whose scan is running.
	scanningCell = "…"
	// partialMarker prefixes a figure that covers only part of what the prefix holds.
	partialMarker = ">"
)

// unknownCells are the cells of a row that has not been scanned.
func unknownCells() statCells {
	return statCells{unknownCell, unknownCell, unknownCell, unknownCell}
}

// scanningCells are the cells of a row whose scan is running.
func scanningCells() statCells {
	return statCells{scanningCell, scanningCell, scanningCell, scanningCell}
}

// cellsOf renders an aggregate, or the unknown cells for a scan that produced none. A partial
// aggregate is marked on the figures it understates, so it cannot be read as a total.
func cellsOf(stats *s3.PrefixStats) statCells {
	if stats == nil {
		return unknownCells()
	}

	marker := ""
	if stats.Partial {
		marker = partialMarker
	}

	return statCells{
		objects:      marker + util.FormatNumber(int64(stats.Objects)),
		size:         marker + util.FormatBytes(stats.Size),
		lastModified: s3.FormatTime(stats.LastModified),
		storageClass: stats.StorageClass(),
	}
}

// renderStatCells writes cells into the aggregate columns of entry's row, starting at firstCol.
//
// A scan that finishes after the user re-filtered the page may find its row gone from the view;
// the value stays on the row itself, so the next render of that row picks it up.
func renderStatCells[R comparable](
	table *tview.Table,
	visible []R,
	entry R,
	firstCol int,
	cells statCells,
) {
	texts := []string{cells.objects, cells.size, cells.lastModified, cells.storageClass}

	for i, candidate := range visible {
		if candidate != entry {
			continue
		}
		for offset, text := range texts {
			if cell := table.GetCell(i+1, firstCol+offset); cell != nil {
				cell.SetText(text)
			}
		}
		return
	}
}

// ScanPrefix aggregates everything under bucket/prefix and hands the result to done on the UI
// goroutine. An empty prefix aggregates the whole bucket. done is called with nil when the scan
// was cancelled or failed, so the row it belongs to can go back to showing no aggregate.
//
// How deep the walk goes is the user's setting alone: s3.max_scanned_keys, zero for no cap.
//
// Only one scan runs at a time: it walks the whole subtree, so letting them pile up would spend
// the user's requests on rows they are no longer looking at. CancelScan stops the running one.
func (app *App) ScanPrefix(bucket, prefix string, done func(*s3.PrefixStats)) {
	path := s3.DisplayPath(bucket, prefix)

	if !app.beginScan() {
		SendStatusWithDefaultTTL("[red]a scan is already running, press <Esc> to cancel it")
		return
	}

	// A scan spends one request per page of keys, so the single-call timeout does not apply to it:
	// it ends when it is done, when the user cancels it, or when the application exits.
	ctx, cancel := context.WithCancel(app.ctx)
	app.setScanCancel(cancel)

	SendStatusInfinite("scanning " + path + " (<Esc> to cancel)")

	go func() {
		defer cancel()
		defer app.endScan()

		// The row hears about every outcome: a cancelled or failed scan has to take the
		// "scanning" cells back off it.
		report := func(stats *s3.PrefixStats) {
			app.QueueUpdateDraw(func() { done(stats) })
		}

		client, err := app.S3Client(ctx)
		if err != nil {
			failed("scanning "+path, err)
			report(nil)
			return
		}

		stats, err := client.StatPrefix(ctx, bucket, prefix, scanProgress(path))
		switch {
		case errors.Is(err, context.Canceled):
			SendStatusWithDefaultTTL("scan of " + path + " cancelled")
			report(nil)
		case err != nil:
			failed("scanning "+path, err)
			report(nil)
		default:
			SendStatusWithDefaultTTL(scanSummary(path, stats))
			report(stats)
		}
	}()
}

// scanProgressPages is how many pages of keys pass between progress reports. Reporting every page
// would redraw the status line hundreds of times for no added information.
const scanProgressPages = 10

// scanProgress reports how far a scan has got, every scanProgressPages pages.
func scanProgress(label string) func(int) {
	pages := 0
	return func(scanned int) {
		pages++
		if pages%scanProgressPages != 0 {
			return
		}
		SendStatusInfinite(
			fmt.Sprintf(
				"scanning %s — %s keys (<Esc> to cancel)",
				label,
				util.FormatNumber(int64(scanned)),
			),
		)
	}
}

// scanSummary reports what a finished scan found.
func scanSummary(path string, stats *s3.PrefixStats) string {
	summary := fmt.Sprintf(
		"%s: %s objects, %s",
		path,
		util.FormatNumber(int64(stats.Objects)),
		util.FormatBytes(stats.Size),
	)
	if stats.Partial {
		summary += " (partial: scanned-keys cap reached)"
	}
	return summary
}

// CancelScan stops the running scan, if any, and reports whether there was one.
func (app *App) CancelScan() bool {
	app.scanMu.Lock()
	cancel := app.scanCancel
	app.scanCancel = nil
	app.scanMu.Unlock()

	if cancel == nil {
		return false
	}
	cancel()
	return true
}

// beginScan claims the single scan slot, reporting whether it was free.
func (app *App) beginScan() bool {
	app.scanMu.Lock()
	defer app.scanMu.Unlock()

	if app.scanning {
		return false
	}
	app.scanning = true
	return true
}

// endScan releases the scan slot.
func (app *App) endScan() {
	app.scanMu.Lock()
	defer app.scanMu.Unlock()

	app.scanning = false
	app.scanCancel = nil
}

// setScanCancel records how to cancel the running scan.
func (app *App) setScanCancel(cancel context.CancelFunc) {
	app.scanMu.Lock()
	defer app.scanMu.Unlock()

	app.scanCancel = cancel
}
