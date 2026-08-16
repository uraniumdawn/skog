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
	"text/tabwriter"

	"github.com/gdamore/tcell/v2"

	"github.com/uraniumdawn/skog/pkg/iceberg"
	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// icebergVersions is what one listing of a table's metadata level produced.
type icebergVersions struct {
	versions []iceberg.Version
	// truncated reports that the level was walked only as far as the configured cap, so a newer
	// document may exist and not be listed.
	truncated bool
}

// IcebergTable opens a table: the metadata documents it holds, newest first.
//
// Which of them a writer last committed is not in the bucket unless the table keeps a version
// hint there — every other catalog holds that pointer itself — so the newest is marked rather
// than opened, and the title says on what basis.
func (app *App) IcebergTable(target IcebergTableTarget) {
	pageKey := app.icebergTablePageKey(target)
	display := s3.DisplayPath(target.Bucket, target.Prefix)

	fetch(app, "listing the metadata of "+display,
		func(ctx context.Context, client *s3.Client) (icebergVersions, error) {
			versions, truncated, err := iceberg.Versions(
				ctx,
				iceberg.From(client),
				target.Bucket,
				target.Prefix,
				app.Config.GetMaxScannedKeys(),
			)
			return icebergVersions{versions: versions, truncated: truncated}, err
		},
		func(found icebergVersions) {
			app.showIcebergTable(pageKey, display, target, found)
		},
	)
}

func (app *App) showIcebergTable(
	pageKey, display string,
	target IcebergTableTarget,
	found icebergVersions,
) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	versions := found.versions
	visible := versions

	table := app.newIcebergTable()
	title := versionsTitle(display, found)

	render := func(filter string) {
		cells, index := filterIcebergRows(icebergVersionCells(versions), filter)
		visible = make([]iceberg.Version, 0, len(index))
		for _, i := range index {
			visible = append(visible, versions[i])
		}
		fillIcebergTable(
			table,
			[]string{"Version", "File", "Modified", "Size", "Current"},
			cells,
			labelColor,
		)
		util.SetSearchableTitle(table, title, filter)
	}
	render("")

	selected := func() (IcebergVersionTarget, bool) {
		row, _ := table.GetSelection()
		if row < 1 || row > len(visible) {
			return IcebergVersionTarget{}, false
		}
		return IcebergVersionTarget{
			Bucket: target.Bucket,
			Prefix: target.Prefix,
			Key:    visible[row-1].Key,
		}, true
	}

	// open descends into the snapshots the document under the cursor lists.
	open := func() {
		if version, ok := selected(); ok {
			Publish(IcebergChannel, GetIcebergSnapshotsEventType, Payload{version, false})
		}
	}

	// up opens the level the table sits in.
	up := func() {
		parent, _ := s3.Parent(target.Prefix)
		Publish(
			IcebergChannel,
			GetIcebergLevelEventType,
			Payload{IcebergLevelTarget{Bucket: target.Bucket, Prefix: parent}, false},
		)
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			Publish(IcebergChannel, GetIcebergTableEventType, Payload{target, true})
			return nil
		}

		// <i> and <s> are off the chain <h>/<l> walk: they describe the document under the
		// cursor rather than sit under it, the way an object's metadata and a file's schema do.
		if IsKey(event, 'i') {
			if version, ok := selected(); ok {
				Publish(IcebergChannel, GetIcebergOverviewEventType, Payload{version, false})
			}
			return nil
		}

		if IsKey(event, 's') {
			if version, ok := selected(); ok {
				Publish(IcebergChannel, GetIcebergSchemaEventType, Payload{version, false})
			}
			return nil
		}

		return event
	})

	app.AddToPagesRegistry(pageKey, table, IcebergTablePageMenu, true)
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, up, open)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})
}

// versionsTitle names the table, how many documents it holds and what the Current column means.
//
// It says outright that no catalog was consulted: a mark that came from the file names is a
// reading of the bucket, not of what a writer committed, and the difference is the whole reason
// the page lists every document rather than opening one.
func versionsTitle(display string, found icebergVersions) string {
	basis := "no catalog, newest by name"
	for _, version := range found.versions {
		if version.Mark == iceberg.MarkHint {
			basis = "version-hint.text"
			break
		}
	}

	count := strconv.Itoa(len(found.versions))
	if found.truncated {
		count = partialMarker + count
	}
	return fmt.Sprintf(" %s metadata [%s] (%s) ", display, count, basis)
}

// icebergVersionCells renders the documents as the cells the table shows.
func icebergVersionCells(versions []iceberg.Version) [][]string {
	cells := make([][]string, 0, len(versions))
	for _, version := range versions {
		number := unknownCell
		if version.Number >= 0 {
			number = strconv.Itoa(version.Number)
		}
		cells = append(cells, []string{
			number,
			version.File,
			s3.FormatTime(version.Modified),
			util.FormatBytes(version.Size),
			version.Mark,
		})
	}
	return cells
}

// IcebergOverview opens what one metadata document says about the table itself.
func (app *App) IcebergOverview(target IcebergVersionTarget) {
	pageKey := app.icebergOverviewPageKey(target)
	display := s3.DisplayPath(target.Bucket, target.Prefix)

	fetch(app, "reading "+path.Base(target.Key),
		func(ctx context.Context, client *s3.Client) (*iceberg.Metadata, error) {
			return iceberg.LoadMetadata(ctx, iceberg.From(client), target.Bucket, target.Key)
		},
		func(metadata *iceberg.Metadata) {
			view := app.NewDescription(fmt.Sprintf(" %s@%s ", display, path.Base(target.Key)))
			view.SetText(overviewText(target, metadata))
			view.SetInputCapture(
				app.WithHScroll(view, func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyCtrlU {
						Publish(
							IcebergChannel,
							GetIcebergOverviewEventType,
							Payload{target, true},
						)
						return nil
					}
					return event
				}),
			)

			app.AddToPagesRegistry(pageKey, view, IcebergOverviewPageMenu, false)
			// The overview describes one document, so above it is the list of them. Nothing
			// hangs off it: the data is under the snapshots, which <l> reaches from that list.
			app.Layout.PagesRegistry.SetPageNavigation(pageKey, app.leaveVersion(target), nil)
		},
	)
}

// leaveVersion is what <h> does on a page describing one metadata document: back to the list of
// them.
func (app *App) leaveVersion(target IcebergVersionTarget) func() {
	return func() {
		Publish(
			IcebergChannel,
			GetIcebergTableEventType,
			Payload{IcebergTableTarget{Bucket: target.Bucket, Prefix: target.Prefix}, false},
		)
	}
}

// overviewText lays the table's own properties out, a field to a line.
func overviewText(target IcebergVersionTarget, metadata *iceberg.Metadata) string {
	var sb strings.Builder
	w := tabwriter.NewWriter(&sb, 0, 0, 2, ' ', 0)

	_, _ = fmt.Fprintf(w, "Table:\t%s\n", tableName(target.Prefix))
	_, _ = fmt.Fprintf(w, "Metadata:\t%s\n", path.Base(target.Key))
	_, _ = fmt.Fprintf(w, "Format version:\t%d\n", metadata.FormatVersion)
	writeIcebergField(w, "Table UUID", metadata.TableUUID)
	writeIcebergField(w, "Location", metadata.Location)
	_, _ = fmt.Fprintf(w, "Last updated:\t%s\n", s3.FormatTime(metadata.LastUpdated()))

	if metadata.CurrentSnapshotID != nil {
		_, _ = fmt.Fprintf(w, "Current snapshot:\t%d\n", *metadata.CurrentSnapshotID)
	} else {
		_, _ = fmt.Fprintf(w, "Current snapshot:\t%s\n", "none")
	}
	_, _ = fmt.Fprintf(w, "Snapshots:\t%d\n", len(metadata.Snapshots))

	schema := metadata.CurrentSchema()
	if schema != nil {
		_, _ = fmt.Fprintf(w, "Schema:\t%d columns (schema-id %d)\n",
			len(schema.Fields), schema.SchemaID)
	}
	writeIcebergField(w, "Partition spec", specText(metadata.DefaultPartitionSpec(), schema))
	writeIcebergField(w, "Sort order", sortOrderText(metadata.DefaultSortOrder(), schema))

	if len(metadata.Refs) > 0 {
		_, _ = fmt.Fprintln(w, "")
		_, _ = fmt.Fprintf(w, "Refs:\t%d\n", len(metadata.Refs))
		for _, name := range util.SortedKeys(metadata.Refs) {
			ref := metadata.Refs[name]
			_, _ = fmt.Fprintf(w, "  %s:\t%s %d\n", name, ref.Type, ref.SnapshotID)
		}
	}

	if len(metadata.Properties) > 0 {
		_, _ = fmt.Fprintln(w, "")
		_, _ = fmt.Fprintf(w, "Properties:\t%d\n", len(metadata.Properties))
		for _, name := range util.SortedKeys(metadata.Properties) {
			_, _ = fmt.Fprintf(w, "  %s:\t%s\n", name, metadata.Properties[name])
		}
	}

	_ = w.Flush()
	return sb.String()
}

// writeIcebergField writes a labelled line, leaving out a field the document does not carry.
func writeIcebergField(w *tabwriter.Writer, label, value string) {
	if value == "" {
		return
	}
	_, _ = fmt.Fprintf(w, "%s:\t%s\n", label, value)
}

// specText renders a partition spec as the transforms it applies: "dt = day(ts)". The column a
// transform reads is named where the schema knows it, and by its field id where it does not — a
// spec may outlive the column it was written against.
func specText(spec *iceberg.PartitionSpec, schema *iceberg.Schema) string {
	if spec == nil || len(spec.Fields) == 0 {
		return ""
	}

	parts := make([]string, 0, len(spec.Fields))
	for _, field := range spec.Fields {
		parts = append(parts, fmt.Sprintf(
			"%s = %s(%s)", field.Name, field.Transform, columnName(schema, field.SourceID),
		))
	}
	return strings.Join(parts, ", ")
}

// sortOrderText renders a sort order as its terms: "ts desc nulls-last".
func sortOrderText(order *iceberg.SortOrder, schema *iceberg.Schema) string {
	if order == nil || len(order.Fields) == 0 {
		return ""
	}

	parts := make([]string, 0, len(order.Fields))
	for _, field := range order.Fields {
		term := columnName(schema, field.SourceID)
		if field.Transform != "" && field.Transform != "identity" {
			term = field.Transform + "(" + term + ")"
		}
		if field.Direction != "" {
			term += " " + field.Direction
		}
		if field.NullOrder != "" {
			term += " " + field.NullOrder
		}
		parts = append(parts, term)
	}
	return strings.Join(parts, ", ")
}

// columnName is what a field id is called in the schema, and the id itself where the schema does
// not name it.
func columnName(schema *iceberg.Schema, id int) string {
	if schema != nil {
		for _, field := range schema.Fields {
			if field.ID == id {
				return field.Name
			}
		}
	}
	return "field-" + strconv.Itoa(id)
}

// IcebergSchema opens the schema one metadata document declares.
func (app *App) IcebergSchema(target IcebergVersionTarget) {
	pageKey := app.icebergSchemaPageKey(target)
	display := s3.DisplayPath(target.Bucket, target.Prefix)

	fetch(app, "reading the schema of "+path.Base(target.Key),
		func(ctx context.Context, client *s3.Client) (*iceberg.Metadata, error) {
			return iceberg.LoadMetadata(ctx, iceberg.From(client), target.Bucket, target.Key)
		},
		func(metadata *iceberg.Metadata) {
			schema := metadata.CurrentSchema()
			if schema == nil {
				SendStatusWithDefaultTTL("this metadata document declares no schema")
				return
			}
			app.showIcebergSchema(pageKey, display, target, schema)
		},
	)
}

func (app *App) showIcebergSchema(
	pageKey, display string,
	target IcebergVersionTarget,
	schema *iceberg.Schema,
) {
	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	table := app.newIcebergTable()

	title := fmt.Sprintf(
		" %s@%s schema [%d] (schema-id %d) ",
		display, path.Base(target.Key), len(schema.Fields), schema.SchemaID,
	)

	render := func(filter string) {
		cells, _ := filterIcebergRows(icebergSchemaCells(schema), filter)
		fillIcebergTable(
			table,
			[]string{"Id", "Name", "Type", "Required", "Doc"},
			cells,
			labelColor,
		)
		util.SetSearchableTitle(table, title, filter)
	}
	render("")

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyCtrlU {
			Publish(IcebergChannel, GetIcebergSchemaEventType, Payload{target, true})
			return nil
		}
		return event
	})

	app.AddToPagesRegistry(pageKey, table, IcebergSchemaPageMenu, true)
	// A schema describes one document, so above it is the list of them. Nothing hangs off a
	// column.
	app.Layout.PagesRegistry.SetPageNavigation(pageKey, app.leaveVersion(target), nil)

	app.AssignSearch(func(text string) {
		render(text)
		table.ScrollToBeginning()
	})
}

// icebergSchemaCells renders a schema's fields as the cells the table shows.
func icebergSchemaCells(schema *iceberg.Schema) [][]string {
	cells := make([][]string, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		required := "no"
		if field.Required {
			required = "yes"
		}
		cells = append(cells, []string{
			strconv.Itoa(field.ID),
			field.Name,
			iceberg.TypeString(field.Type),
			required,
			field.Doc,
		})
	}
	return cells
}
