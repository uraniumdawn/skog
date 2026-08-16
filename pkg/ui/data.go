// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/sahilm/fuzzy"

	"github.com/uraniumdawn/skog/pkg/format"
	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// ViewData opens the viewer for an object: what the file holds, as a table. It is the level
// under an object's metadata, which is what <l> there descends into.
//
// It reads S3 and writes only skog's own cache, so like a download it is not gated on the
// profile's mode. A format skog has no reader for is refused here, before anything is fetched:
// there is no sense spending a transfer on a file that could not be shown.
//
// It must be called on the UI goroutine, which is where a keypress handler runs.
func (app *App) ViewData(bucket, key string) {
	if reason := unviewable(key); reason != "" {
		SendStatusWithDefaultTTL(reason)
		return
	}

	Publish(S3Channel, GetObjectDataEventType, Payload{ObjectDataTarget{bucket, key}, false})
}

// unviewable says why an object will not be opened, empty for one that will be. It is decided
// from the key alone so that it is decided before the object is fetched.
func unviewable(key string) string {
	kind := format.FromKey(key)

	switch {
	case kind == format.Unknown:
		// The name rather than the key: a key can be long enough to push the message off the
		// status line, and it is the extension that this is about.
		return fmt.Sprintf("no viewer for %s", path.Base(key))
	case !format.Supported(kind):
		return fmt.Sprintf("%s files are not readable yet", kind)
	case kind == format.Parquet && format.Compressed(key):
		return "parquet holds its own compression, so a .gz of one is not read"
	}
	return ""
}

// ObjectData fetches an object's body and opens it as a table of its rows.
//
// The body goes through skog's cache, so the same object opened again costs no transfer. The
// fetch holds the job slot for as long as it runs: an object can be gigabytes, which is longer
// than the single-call timeout allows for and long enough to be worth cancelling with <Esc>.
func (app *App) ObjectData(target ObjectDataTarget) {
	display := s3.DisplayPath(target.Bucket, target.Key)
	what := "viewing " + display

	if app.Bodies == nil {
		SendStatusWithDefaultTTL("[red]the object cache is unavailable; see the log")
		return
	}

	if !app.beginJob("a view") {
		return
	}

	ctx, cancel := context.WithCancel(app.ctx)
	app.setJobCancel(cancel)

	SendStatusInfinite(what + " (<Esc> to cancel)")

	go func() {
		defer cancel()
		defer app.endJob()

		reader, err := app.readObject(ctx, target, display)
		switch {
		// A cancelled read of a body surfaces as whatever the transport made of it, so the
		// context is what says the user is the one who ended it.
		case err != nil && (errors.Is(err, context.Canceled) || ctx.Err() != nil):
			SendStatusWithDefaultTTL("view of " + display + " cancelled")
			return
		case err != nil:
			failed(what, err)
			return
		}

		rows, err := reader.NextBatch()
		if err != nil {
			_ = reader.Close()
			failed(what, err)
			return
		}

		app.QueueUpdateDraw(func() {
			// A columnar file becomes a table; a line-oriented one is shown as it was written.
			if format.Tabular(format.FromKey(target.Key)) {
				app.showObjectData(target, display, reader, rows)
			} else {
				app.showObjectLines(target, display, reader, rows)
			}
			ClearStatus()
		})
	}()
}

// readObject puts the object's body in the cache if it is not there already, and opens a reader
// on it. The reader is the caller's to close.
func (app *App) readObject(
	ctx context.Context,
	target ObjectDataTarget,
	display string,
) (format.Reader, error) {
	client, err := app.S3Client(ctx)
	if err != nil {
		return nil, err
	}

	// The metadata first, for two things the fetch needs: how big the object is, and the ETag
	// that names its body in the cache.
	info, err := client.HeadObject(ctx, target.Bucket, target.Key)
	if err != nil {
		return nil, err
	}

	if limit := app.Config.MaxViewableObjectSize(); info.Size > limit {
		return nil, fmt.Errorf(
			"%s is %s, over the %s the viewer reads whole; <d> downloads it",
			display,
			util.FormatBytes(info.Size),
			util.FormatBytes(limit),
		)
	}

	body := app.Bodies.Path(app.SelectedProfileName(), target.Bucket, target.Key, info.ETag)
	if !app.Bodies.Get(body) {
		SendStatusInfinite(fmt.Sprintf(
			"fetching %s (%s) (<Esc> to cancel)", display, util.FormatBytes(info.Size),
		))

		err := app.Bodies.Put(body, func(dst string) error {
			_, err := client.FetchObject(ctx, target.Bucket, target.Key, dst)
			return err
		})
		if err != nil {
			return nil, err
		}
	}

	return format.Open(target.Key, body, app.Config.ViewerPageRows())
}

// showObjectData builds the viewer page from the first batch of rows.
func (app *App) showObjectData(
	target ObjectDataTarget,
	display string,
	reader format.Reader,
	first [][]string,
) {
	pageKey := app.objectDataPageKey(target)
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)

	columns := reader.Columns()
	rows := first
	// visible is what the table currently shows: the rows themselves under no filter, a subset
	// under one. It is how a table row maps back to the row a keypress acts on.
	visible := rows
	// exhausted is what takes <n> off the menu. A batch short of what was asked for is the end
	// of the file: NextBatch fills a batch unless there is nothing left to fill it with.
	pageRows := app.Config.ViewerPageRows()
	exhausted := len(first) < pageRows

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
	fillDataTable(table, columns, rows, labelColor)

	title := dataTitle(display, len(rows), reader.NumRows())
	util.SetSearchableTitle(table, title, "")

	// render redraws the table for the filter in force, which is also what a new batch needs.
	render := func(filter string) {
		visible = filterDataRows(rows, filter)
		fillDataTable(table, columns, visible, labelColor)
		title = dataTitle(display, len(rows), reader.NumRows())
		util.SetSearchableTitle(table, title, filter)
	}

	// openRecord shows the row the cursor is on in full, which is the level below the table. A
	// row is as far as the file goes: the popup has nothing under it.
	openRecord := func() {
		// The row the cursor is on, nothing on the header or an empty table.
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return
		}

		app.ShowRecord(
			fmt.Sprintf(" %s — row %s ", display, util.FormatNumber(int64(row))),
			recordText(columns, visible[row-1]),
		)
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// A file has as many columns as it has, and rarely as few as fit the screen. <Left> and
		// <Right> already scroll the table sideways, since it selects rows and not columns; the
		// vim pair for that is <h>/<l>, which skog spends on the hierarchy. <H>/<L> are handed on
		// as the arrows they stand for, so the scrolling and its bounds stay tview's.
		if IsKey(event, 'H') {
			return tcell.NewEventKey(tcell.KeyLeft, 0, tcell.ModNone)
		}
		if IsKey(event, 'L') {
			return tcell.NewEventKey(tcell.KeyRight, 0, tcell.ModNone)
		}

		if event.Key() == tcell.KeyCtrlU {
			Publish(S3Channel, GetObjectDataEventType, Payload{target, true})
			return nil
		}

		if IsKey(event, 's') {
			app.showObjectSchema(target, display, reader.Schema())
			return nil
		}

		if IsKey(event, 'n') {
			// <n> is off the menu once the file is read out, so this only guards a stale
			// keypress.
			if exhausted {
				return nil
			}

			// Reading a batch is decompression off a local file, not a request, so it runs
			// here rather than through the job slot.
			batch, err := reader.NextBatch()
			if err != nil {
				failed("reading "+display, err)
				return nil
			}
			if len(batch) < pageRows {
				exhausted = true
			}
			rows = append(rows, batch...)

			render(app.CurrentFilters[pageKey])
			app.SetPageMenu(pageKey, rowsMenu(exhausted))
			return nil
		}

		return event
	})

	app.AddToPagesRegistry(pageKey, table, rowsMenu(exhausted), true)
	// The reader holds the cached file open for as long as the page shows it, and the page is
	// kept for the session; removing the page is what closes it.
	app.Layout.PagesRegistry.SetPageCloser(pageKey, reader)
	// Above the rows is the object they were read from, below them one row in full. The schema
	// is off this chain: it describes the rows rather than sitting under them, and <s> is what
	// opens it.
	app.Layout.PagesRegistry.SetPageNavigation(pageKey,
		func() {
			Publish(
				S3Channel,
				GetObjectEventType,
				Payload{ObjectTarget{Bucket: target.Bucket, Key: target.Key}, false},
			)
		},
		openRecord,
	)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})
}

// NewRecordPage builds the popup one row is shown in full in. It is built once and filled in
// each time a row is opened, the way the Resources and Opened-pages modals are.
func (app *App) NewRecordPage() tview.Primitive {
	view := app.NewLines()
	// The one place in the viewer that wraps. A table cell stops at the edge of the screen and
	// <H>/<L> only move between columns, so a value wider than the terminal is unreadable
	// until it is broken across lines — which is what this popup is for.
	view.SetWrap(true)
	closeRecord := func() {
		app.HideModalPage(Record)
		// Back to the page the popup was opened over, which is the front one again.
		app.SetFocus(app.Layout.PagesRegistry.UI.Pages)
	}

	view.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			closeRecord()
			return nil
		}
		return event
	})

	// A row is the bottom of the chain: <h> is the way back up out of it, the same key that
	// leaves every other level, and there is nothing below to descend into.
	app.Layout.PagesRegistry.SetPageNavigation(Record, closeRecord, nil)

	app.record = view
	return util.NewRecordModal(view)
}

// ShowRecord opens the popup on one row.
//
// It must be called on the UI goroutine.
func (app *App) ShowRecord(title, body string) {
	app.record.SetTitle(title)
	app.record.SetText(body)
	app.record.ScrollToBeginning()

	app.ShowModalPage(Record)
	// Focus follows the page in front, so the keys the popup answers to reach it rather than
	// the table it is covering.
	app.SetFocus(app.Layout.PagesRegistry.UI.Pages)
}

// recordText lays a row out as a field to a line, under the same names the table heads its
// columns with.
//
// Names are padded to a common width so the values line up. A value too long for the popup
// wraps to the left margin rather than under its own column: tview has no hanging indent, and
// a value that has to wrap is being read for what it says, not for where it starts.
func recordText(columns []format.Column, row []string) string {
	width := 0
	for _, column := range columns {
		if w := utf8.RuneCountInString(column.Name); w > width {
			width = w
		}
	}

	var out strings.Builder
	for i, column := range columns {
		value := ""
		// A row shorter than the header is a file that changed under the page; showing the
		// columns that are there beats showing none of them.
		if i < len(row) {
			value = row[i]
		}
		fmt.Fprintf(&out, "%-*s  %s\n", width, column.Name, value)
	}
	return out.String()
}

// showObjectSchema opens the schema of the file being viewed, as the file itself declares it.
//
// It is a page of its own rather than a pane on the viewer: a schema is read once and then got
// out of the way. It is off the chain <h>/<l> walk — it describes the rows rather than sitting
// under them — so <s> is what opens it, and <h> what leaves it back to the rows.
//
// It must be called on the UI goroutine.
func (app *App) showObjectSchema(target ObjectDataTarget, display, schema string) {
	if schema == "" {
		SendStatusWithDefaultTTL("this file declares no schema")
		return
	}

	pageKey := app.objectSchemaPageKey(target)
	if _, found := app.Cache.Get(pageKey); found {
		app.SwitchToPage(pageKey)
		return
	}

	// The same view a record is shown in: a schema is JSON, brackets and all, and wrapping it
	// would break the layout that makes it readable.
	view := app.NewLines()
	view.SetTitle(fmt.Sprintf(" %s schema ", display))
	view.SetText(schema)
	view.SetInputCapture(app.WithHScroll(view, func(event *tcell.EventKey) *tcell.EventKey {
		return event
	}))

	app.AddToPagesRegistry(pageKey, view, ObjectSchemaPageMenu, false)
	// A schema is read from the file the rows came from, so above it are those rows. Nothing
	// hangs off a schema.
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, func() {
		Publish(S3Channel, GetObjectDataEventType, Payload{target, false})
	}, nil)
}

// showObjectLines builds the viewer page for a line-oriented file: the lines themselves, as
// they were written.
//
// Nothing is parsed into columns. A JSON record is written by whoever wrote it, and the order
// of its keys, the shape it takes and the fields it happens to carry are the reasons to open
// the file at all — a table would reorder, reformat and drop them.
func (app *App) showObjectLines(
	target ObjectDataTarget,
	display string,
	reader format.Reader,
	first [][]string,
) {
	pageKey := app.objectDataPageKey(target)

	lines := flatten(first)
	pageRows := app.Config.ViewerPageRows()
	exhausted := len(first) < pageRows

	view := app.NewLines()
	render := func(filter string) {
		view.SetText(strings.Join(filterLines(lines, filter), "\n"))
		util.SetSearchableTitle(view, dataTitle(display, len(lines), reader.NumRows()), filter)
	}
	render("")

	view.SetInputCapture(app.WithHScroll(view, func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			Publish(S3Channel, GetObjectDataEventType, Payload{target, true})
			return nil
		}

		if IsKey(event, 'n') {
			// <n> is off the menu once the file is read out, so this only guards a stale
			// keypress.
			if exhausted {
				return nil
			}

			// Reading a batch is a read of a local file, not a request, so it runs here rather
			// than through the job slot.
			batch, err := reader.NextBatch()
			if err != nil {
				failed("reading "+display, err)
				return nil
			}
			if len(batch) < pageRows {
				exhausted = true
			}
			lines = append(lines, flatten(batch)...)

			render(app.CurrentFilters[pageKey])
			app.SetPageMenu(pageKey, linesMenu(exhausted))
			return nil
		}

		return event
	}))

	app.AddToPagesRegistry(pageKey, view, linesMenu(exhausted), true)
	// The reader holds the cached file open for as long as the page shows it, and the page is
	// kept for the session; removing the page is what closes it.
	app.Layout.PagesRegistry.SetPageCloser(pageKey, reader)
	// Above the lines is the object they were read from. Nothing hangs off them: a
	// line-oriented file declares no schema, which is the level a table has below it.
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, func() {
		Publish(
			S3Channel,
			GetObjectEventType,
			Payload{ObjectTarget{Bucket: target.Bucket, Key: target.Key}, false},
		)
	}, nil)

	app.AssignSearch(func(text string) {
		render(text)
		view.ScrollToBeginning()
	})
}

// NewLines builds the view a line-oriented file is shown in.
//
// Colour tags are off, unlike every other text page: a JSON record holds brackets of its own,
// and ["a","b"] read as a tview region tag would be swallowed rather than shown. Wrapping is
// off too, so a long record stays one line and <H>/<L> scroll along it.
func (app *App) NewLines() *tview.TextView {
	view := tview.NewTextView()
	view.SetDynamicColors(false).
		SetWrap(false)
	view.SetBorder(true).
		SetBorderPadding(0, 0, 1, 0)
	view.SetTextColor(tcell.GetColor(app.Colors.Skog.Foreground))
	return view
}

// flatten takes the single cell of each row of a line-oriented batch.
func flatten(rows [][]string) []string {
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		lines = append(lines, row[0])
	}
	return lines
}

// filterLines keeps the lines holding filter, ignoring case. An empty filter keeps all of them.
//
// Plain containment, not the fuzzy match the other pages use: those filter short names, where
// skipping letters is what makes a filter quick. A record here is hundreds of characters of
// JSON, and a fuzzy match over one finds its letters scattered through punctuation and matches
// nearly every line. What is wanted of a record is grep.
func filterLines(lines []string, filter string) []string {
	if filter == "" {
		return lines
	}

	needle := strings.ToLower(filter)
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.Contains(strings.ToLower(line), needle) {
			filtered = append(filtered, line)
		}
	}
	return filtered
}

// dataTitle names the object and how much of it is loaded. total is -1 for a format that
// cannot say how many rows it holds without reading all of them.
func dataTitle(display string, loaded int, total int64) string {
	if total < 0 {
		return fmt.Sprintf(" %s [%s] ", display, util.FormatNumber(int64(loaded)))
	}
	return fmt.Sprintf(
		" %s [%s/%s] ", display, util.FormatNumber(int64(loaded)), util.FormatNumber(total),
	)
}

// rowsMenu is the menu of a file shown as a table: the one offering <s> for its schema, and
// <n> only while rows are left.
func rowsMenu(exhausted bool) string {
	if exhausted {
		return ObjectRowsPageMenu
	}
	return ObjectRowsBatchedPageMenu
}

// linesMenu is the menu of a file shown as its lines. It offers no <s>: a line format declares
// no schema to show.
func linesMenu(exhausted bool) string {
	if exhausted {
		return ObjectLinesPageMenu
	}
	return ObjectLinesBatchedPageMenu
}

// fillDataTable rebuilds the table from the rows given, under the file's own column names.
func fillDataTable(
	table *tview.Table,
	columns []format.Column,
	rows [][]string,
	labelColor tcell.Color,
) {
	table.Clear()

	headers := make([]string, 0, len(columns))
	for _, c := range columns {
		headers = append(headers, c.Name)
	}
	util.SetTableHeaders(table, labelColor, headers...)

	// No width cap on a cell. A column is as wide as the widest value on screen, and the only
	// thing that shortens one is the edge of the terminal — which <H>/<L> move, by bringing the
	// column to the left of the screen where the whole width is its own.
	for i, row := range rows {
		for j, cell := range row {
			table.SetCell(i+1, j, tview.NewTableCell(cell))
		}
	}
}

// filterDataRows keeps the rows matching filter, an empty filter keeping all of them.
//
// A row matches on any of its cells, since which column holds what is being looked for is
// exactly what a file being read for the first time does not say.
func filterDataRows(rows [][]string, filter string) [][]string {
	if filter == "" {
		return rows
	}

	haystack := make([]string, len(rows))
	for i, row := range rows {
		haystack[i] = strings.Join(row, " ")
	}

	matches := fuzzy.Find(filter, haystack)
	filtered := make([][]string, 0, len(matches))
	for _, m := range matches {
		filtered = append(filtered, rows[m.Index])
	}
	return filtered
}
