// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/compress"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

// writeParquet builds a small parquet file covering what a reader has to survive: a plain
// column, a nullable one, and a nested one. rows is written in chunks so the file has more
// than one row group.
func writeParquet(t *testing.T, rowGroups int) string {
	t.Helper()

	schema := arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "name", Type: arrow.BinaryTypes.String, Nullable: true},
		{Name: "tags", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true},
	}, nil)

	path := filepath.Join(t.TempDir(), "data.parquet")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("creating the file: %v", err)
	}

	props := parquet.NewWriterProperties(parquet.WithCompression(compress.Codecs.Snappy))
	w, err := pqarrow.NewFileWriter(schema, f, props, pqarrow.DefaultWriterProps())
	if err != nil {
		t.Fatalf("opening the writer: %v", err)
	}

	for g := range rowGroups {
		// Between chunks, not after the last one: a row group started and never written to is
		// one the writer refuses to close.
		if g > 0 {
			w.NewRowGroup()
		}

		b := array.NewRecordBuilder(memory.DefaultAllocator, schema)

		ids := b.Field(0).(*array.Int64Builder)
		names := b.Field(1).(*array.StringBuilder)
		tags := b.Field(2).(*array.ListBuilder)
		tagValues := tags.ValueBuilder().(*array.StringBuilder)

		for i := range 2 {
			n := int64(g*2 + i)
			ids.Append(n)

			// The second row of every group is null, so nulls are exercised wherever the
			// reader happens to stop.
			if i == 1 {
				names.AppendNull()
				tags.AppendNull()
				continue
			}
			names.Append("row-" + string(rune('a'+n)))
			tags.Append(true)
			tagValues.AppendValues([]string{"x", "y"}, nil)
		}

		rec := b.NewRecordBatch()
		if err := w.WriteBuffered(rec); err != nil {
			t.Fatalf("writing a record: %v", err)
		}
		rec.Release()
		b.Release()
	}

	// Closing the writer closes the file it was given.
	if err := w.Close(); err != nil {
		t.Fatalf("closing the writer: %v", err)
	}
	return path
}

func TestParquetIsWhatItSaysItIs(t *testing.T) {
	path := writeParquet(t, 1)

	head, tail, err := Magic(path)
	if err != nil {
		t.Fatalf("Magic: %v", err)
	}
	if err := Verify(Parquet, head, tail); err != nil {
		t.Errorf("a file arrow wrote failed verification: %v", err)
	}
}

func TestParquetColumns(t *testing.T) {
	r, err := Open("data.parquet", writeParquet(t, 1), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	cols := r.Columns()
	if len(cols) != 3 {
		t.Fatalf("got %d columns, want 3", len(cols))
	}

	for i, want := range []string{"id", "name", "tags"} {
		if cols[i].Name != want {
			t.Errorf("column %d is %q, want %q", i, cols[i].Name, want)
		}
		if cols[i].Type == "" {
			t.Errorf("column %q has no type", cols[i].Name)
		}
	}

	if cols[0].Nullable {
		t.Error("id is reported nullable")
	}
	if !cols[1].Nullable {
		t.Error("name is not reported nullable")
	}
}

func TestParquetRows(t *testing.T) {
	r, err := Open("data.parquet", writeParquet(t, 1), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	if got := r.NumRows(); got != 2 {
		t.Errorf("NumRows = %d, want 2", got)
	}

	batch, err := r.NextBatch()
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("got %d rows, want 2", len(batch))
	}

	if batch[0][0] != "0" {
		t.Errorf("id = %q, want %q", batch[0][0], "0")
	}
	if batch[0][1] != "row-a" {
		t.Errorf("name = %q, want %q", batch[0][1], "row-a")
	}
	// A nested value is a cell like any other, rendered rather than skipped.
	if batch[0][2] == "" || batch[0][2] == NullValue {
		t.Errorf("nested list rendered as %q", batch[0][2])
	}

	// A null is told apart from an empty string, which for a data file is the whole point.
	if batch[1][1] != NullValue {
		t.Errorf("null name rendered as %q, want %q", batch[1][1], NullValue)
	}
	if batch[1][2] != NullValue {
		t.Errorf("null list rendered as %q, want %q", batch[1][2], NullValue)
	}

	// The file is done, and says so rather than repeating itself.
	rest, err := r.NextBatch()
	if err != nil {
		t.Fatalf("NextBatch at the end: %v", err)
	}
	if rest != nil {
		t.Errorf("got %d rows past the end", len(rest))
	}
}

// Every row comes back exactly once, however the batches fall across the row groups.
func TestParquetBatchesCoverEveryRow(t *testing.T) {
	const groups = 5 // 10 rows

	for _, batchRows := range []int{1, 3, 10, 100} {
		r, err := Open("data.parquet", writeParquet(t, groups), batchRows)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		var ids []string
		for {
			batch, err := r.NextBatch()
			if err != nil {
				t.Fatalf("NextBatch: %v", err)
			}
			if batch == nil {
				break
			}
			if len(batch) > batchRows {
				t.Errorf("batch of %d rows, over the %d asked for", len(batch), batchRows)
			}
			for _, row := range batch {
				ids = append(ids, row[0])
			}
		}
		_ = r.Close()

		if len(ids) != groups*2 {
			t.Fatalf("batchRows=%d: got %d rows, want %d", batchRows, len(ids), groups*2)
		}
		for i, id := range ids {
			if want := strconv.Itoa(i); id != want {
				t.Errorf("batchRows=%d: row %d has id %q, want %q", batchRows, i, id, want)
			}
		}
	}
}

func TestOpenRejectsSomethingElse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not.parquet")
	if err := os.WriteFile(path, []byte("this is not a parquet file"), 0o644); err != nil {
		t.Fatal(err)
	}

	r, err := Open("not.parquet", path, 10)
	if err == nil {
		_ = r.Close()
		t.Fatal("Open accepted a file that is not parquet")
	}
}

func TestOpenRejectsAnUnsupportedFormat(t *testing.T) {
	if _, err := Open("x.avro", writeParquet(t, 1), 10); err == nil {
		t.Error("Open accepted a format with no reader")
	}
}

// Parquet has no schema document of its own, so what is shown is its footer's tree rendered as
// JSON: the physical type, the logical type it stands for, and the repetition that tells a
// nullable column from a required one.
func TestParquetSchemaIsRenderedFromTheFooter(t *testing.T) {
	r, err := Open("data.parquet", writeParquet(t, 1), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	got := r.Schema()
	if !json.Valid([]byte(got)) {
		t.Fatalf("schema is not valid JSON:\n%s", got)
	}

	var root struct {
		Name   string `json:"name"`
		Fields []struct {
			Name        string `json:"name"`
			Repetition  string `json:"repetition"`
			Type        string `json:"type"`
			LogicalType string `json:"logicalType"`
		} `json:"fields"`
	}
	if err := json.Unmarshal([]byte(got), &root); err != nil {
		t.Fatalf("unmarshalling the schema: %v\n%s", err, got)
	}

	if len(root.Fields) != 3 {
		t.Fatalf("got %d fields, want 3:\n%s", len(root.Fields), got)
	}

	id := root.Fields[0]
	if id.Name != "id" || id.Type != "INT64" {
		t.Errorf("id is %+v, want an INT64", id)
	}
	// A required column is what a non-nullable one is stored as.
	if id.Repetition != "required" {
		t.Errorf("id repetition is %q, want required", id.Repetition)
	}

	name := root.Fields[1]
	if name.Repetition != "optional" {
		t.Errorf("name repetition is %q, want optional", name.Repetition)
	}
	// A string is byte array plus a logical type saying what those bytes are.
	if name.Type != "BYTE_ARRAY" || !strings.Contains(name.LogicalType, "String") {
		t.Errorf("name is %+v, want a BYTE_ARRAY carrying a String logical type", name)
	}
}
