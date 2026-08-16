// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
)

// NullValue is how a null is shown. A data file distinguishes a null from an empty string, and
// so does the table: telling them apart is often the reason to look at the file at all.
const NullValue = "null"

// Column describes one column of a data file, for the table header and the schema view.
type Column struct {
	Name string
	// Type is the file's own type for the column, as its schema names it.
	Type     string
	Nullable bool
}

// Reader is a data file read a batch of rows at a time.
//
// Cells are display strings: what a viewer shows is text, and every format has its own idea of
// what a value is, so converting once here keeps that out of the UI.
type Reader interface {
	// Columns is the schema of the file, in the order the rows hold.
	Columns() []Column
	// NextBatch is the next rows, nil once the file is done. A batch holds no more rows than
	// the reader was opened for, and holds fewer only at the end of the file.
	NextBatch() ([][]string, error)
	// NumRows is how many rows the file holds, -1 when the format cannot say without reading
	// all of it.
	NumRows() int64
	// Schema is the file's own schema as JSON, empty for a format that declares none. Avro
	// gives back the schema it was written against; parquet has no schema document of its own,
	// so its footer's tree is rendered as one.
	Schema() string
	io.Closer
}

// Open reads the object cached at path as the format its key names, in batches of batchRows.
//
// It takes the key rather than a Format because the key carries two things: what the object
// holds, and whether it is compressed. The bytes are then checked against that claim, so a key
// that lies is a message naming the problem rather than whatever a parser makes of it.
func Open(key, path string, batchRows int) (Reader, error) {
	f := FromKey(key)

	switch f {
	case Parquet:
		if Compressed(key) {
			// Parquet compresses each column chunk itself, and is read by seeking to its
			// footer; a gzip wrapper around one defeats both and nothing writes them.
			return nil, fmt.Errorf("parquet holds its own compression, so %s is not read", gzSuffix)
		}
		// Parquet is bracketed by its magic, so both ends of the file are worth reading before
		// a parser is pointed at it.
		head, tail, err := Magic(path)
		if err != nil {
			return nil, err
		}
		if err := Verify(f, head, tail); err != nil {
			return nil, err
		}
		return openParquet(path, batchRows)

	case Avro:
		return openAvro(path, Compressed(key), batchRows)

	case NDJSON, JSON:
		// A line format is verified from the front of the stream rather than the file, since
		// what a compressed one begins with is the gzip header and not the record.
		return openLines(f, path, Compressed(key), batchRows)

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, f)
	}
}

// Tabular reports whether the format has columns worth a table.
//
// A columnar file does: its schema names the columns and every row fills the same ones. A
// line-oriented one does not — the record is the line, and it is shown as written.
func Tabular(f Format) bool {
	switch f {
	case Parquet, Avro:
		return true
	default:
		return false
	}
}

// leafColumn is one column of the flattened table: what it is called, and the field indices
// that reach it from the top of a record.
type leafColumn struct {
	Column
	path []int
}

// flattenSchema walks a schema into the columns a table shows.
//
// A record nested in a record is not a value anyone wants to read as one: a struct is opened
// out into its leaves, under the dotted names its fields spell out, the way a warehouse shows
// them. Everything else is a column of its own — a list has a length that changes from record
// to record and so cannot become a fixed set of columns, and is left as it is.
func flattenSchema(schema *arrow.Schema) []leafColumn {
	var leaves []leafColumn
	for i, field := range schema.Fields() {
		leaves = appendLeaves(leaves, field, []int{i}, "", false)
	}
	return leaves
}

// appendLeaves adds the leaves of one field. nullable carries down whether anything above the
// field could be null, since a struct that is null takes everything under it with it.
func appendLeaves(
	leaves []leafColumn,
	field arrow.Field,
	path []int,
	prefix string,
	nullable bool,
) []leafColumn {
	name := prefix + field.Name
	nullable = nullable || field.Nullable

	// A struct with no fields has no leaves to show, so it stays a column of its own.
	structType, ok := field.Type.(*arrow.StructType)
	if !ok || len(structType.Fields()) == 0 {
		return append(leaves, leafColumn{
			Column: Column{Name: name, Type: field.Type.String(), Nullable: nullable},
			path:   slices.Clone(path),
		})
	}

	for i, child := range structType.Fields() {
		leaves = appendLeaves(leaves, child, append(slices.Clone(path), i), name+".", nullable)
	}
	return leaves
}

// cells renders a record as display strings, a row at a time, under the given columns.
//
// Every arrow array can render a value as a string, so a list or a map is a cell like any other
// rather than a case to handle.
func cells(rec arrow.RecordBatch, columns []leafColumn) [][]string {
	rowCount := int(rec.NumRows())

	rows := make([][]string, rowCount)
	for i := range rows {
		rows[i] = make([]string, len(columns))
	}

	// Column at a time: an arrow record is columnar, and this is the order it is laid out in.
	for c, column := range columns {
		parents, leaf := resolve(rec, column.path)
		for i := range rowCount {
			rows[i][c] = value(parents, leaf, i)
		}
	}

	return rows
}

// resolve walks a column's path to the array it reads, collecting the structs passed through on
// the way.
func resolve(rec arrow.RecordBatch, path []int) (parents []arrow.Array, leaf arrow.Array) {
	leaf = rec.Column(path[0])

	for _, i := range path[1:] {
		structArray, ok := leaf.(*array.Struct)
		if !ok {
			// The schema said struct and the data is not one. Showing the value that is there
			// says more than showing nothing.
			return parents, leaf
		}
		parents = append(parents, structArray)
		leaf = structArray.Field(i)
	}

	return parents, leaf
}

// value renders one cell.
//
// A struct that is null holds no fields at that row, whatever its children's arrays happen to
// have at that index, so a null anywhere along the way is what the leaf reads as.
func value(parents []arrow.Array, leaf arrow.Array, i int) string {
	for _, parent := range parents {
		if parent.IsNull(i) {
			return NullValue
		}
	}
	if leaf.IsNull(i) {
		return NullValue
	}
	return leaf.ValueStr(i)
}

// prettyJSON lays out a JSON document for reading, leaving it alone if it does not parse: a
// schema skog cannot format is still a schema worth showing.
func prettyJSON(document string) string {
	var out bytes.Buffer
	if err := json.Indent(&out, []byte(document), "", indent); err != nil {
		return document
	}
	return out.String()
}

// marshalJSON lays out a value as JSON, reporting a failure as the text rather than swallowing
// it: a schema view showing nothing at all says less than one showing why.
func marshalJSON(v any) string {
	out, err := json.MarshalIndent(v, "", indent)
	if err != nil {
		return fmt.Sprintf("the schema could not be rendered: %s", err)
	}
	return string(out)
}

// columnsOf is what a reader shows of its flattened columns: their names and types, without
// the paths that are its own business.
func columnsOf(leaves []leafColumn) []Column {
	columns := make([]Column, 0, len(leaves))
	for _, leaf := range leaves {
		columns = append(columns, leaf.Column)
	}
	return columns
}
