// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
)

// nestedSchema is a record inside a record, beside a list. Two levels is enough to show that
// the walk is a walk and not a single step.
func nestedSchema() *arrow.Schema {
	place := arrow.StructOf(
		arrow.Field{Name: "city", Type: arrow.BinaryTypes.String, Nullable: true},
		arrow.Field{Name: "code", Type: arrow.PrimitiveTypes.Int32, Nullable: true},
	)
	user := arrow.StructOf(
		arrow.Field{Name: "tier", Type: arrow.BinaryTypes.String, Nullable: true},
		arrow.Field{Name: "place", Type: place, Nullable: true},
	)

	return arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "user", Type: user, Nullable: true},
		{Name: "tags", Type: arrow.ListOf(arrow.BinaryTypes.String), Nullable: true},
	}, nil)
}

// writeNestedParquet writes two records: one filled in, one whose user is null outright.
func writeNestedParquet(t *testing.T) string {
	t.Helper()
	schema := nestedSchema()

	b := array.NewRecordBuilder(memory.DefaultAllocator, schema)
	defer b.Release()

	b.Field(0).(*array.Int64Builder).AppendValues([]int64{1, 2}, nil)

	user := b.Field(1).(*array.StructBuilder)
	tier := user.FieldBuilder(0).(*array.StringBuilder)
	place := user.FieldBuilder(1).(*array.StructBuilder)
	city := place.FieldBuilder(0).(*array.StringBuilder)
	code := place.FieldBuilder(1).(*array.Int32Builder)

	// A record with everything set.
	user.Append(true)
	tier.Append("gold")
	place.Append(true)
	city.Append("berlin")
	code.Append(30)

	// A record whose user is null. Its children still hold a slot at that row, and what is in
	// it must not be what the table shows.
	user.AppendNull()
	tier.Append("SHOULD NOT BE SHOWN")
	place.Append(true)
	city.Append("SHOULD NOT BE SHOWN")
	code.Append(99)

	tags := b.Field(2).(*array.ListBuilder)
	tagValues := tags.ValueBuilder().(*array.StringBuilder)
	tags.Append(true)
	tagValues.AppendValues([]string{"a", "b"}, nil)
	tags.AppendNull()

	rec := b.NewRecordBatch()
	defer rec.Release()

	path := filepath.Join(t.TempDir(), "nested.parquet")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	w, err := pqarrow.NewFileWriter(schema, f, parquet.NewWriterProperties(), pqarrow.DefaultWriterProps())
	if err != nil {
		t.Fatalf("opening the writer: %v", err)
	}
	if err := w.WriteBuffered(rec); err != nil {
		t.Fatalf("writing: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing the writer: %v", err)
	}
	return path
}

// A record nested in a record becomes one column per leaf, named by the path down to it. A list
// does not: its length changes from record to record, so it stays a value of its own.
func TestFlattenNamesLeavesByTheirPath(t *testing.T) {
	r, err := Open("nested.parquet", writeNestedParquet(t), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	want := []string{"id", "user.tier", "user.place.city", "user.place.code", "tags"}
	got := names(r.Columns())

	if len(got) != len(want) {
		t.Fatalf("got columns %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("column %d is %q, want %q", i, got[i], want[i])
		}
	}
}

func TestFlattenReadsLeafValues(t *testing.T) {
	r, err := Open("nested.parquet", writeNestedParquet(t), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	rows := drainRows(t, r)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	for i, want := range []string{"1", "gold", "berlin", "30"} {
		if rows[0][i] != want {
			t.Errorf("row 0 column %d is %q, want %q", i, rows[0][i], want)
		}
	}
	// A list is left as it is, since it cannot become a column.
	if rows[0][4] != `["a","b"]` {
		t.Errorf("tags is %q, want the list as it stands", rows[0][4])
	}
}

// The record above the leaves was null, so every column under it reads null — whatever the
// children's own arrays happen to hold at that row.
func TestFlattenNullStructNullsItsLeaves(t *testing.T) {
	r, err := Open("nested.parquet", writeNestedParquet(t), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	rows := drainRows(t, r)
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}

	for _, c := range []int{1, 2, 3} {
		if rows[1][c] != NullValue {
			t.Errorf("column %d under a null record is %q, want %q", c, rows[1][c], NullValue)
		}
	}
	// The columns beside it are untouched.
	if rows[1][0] != "2" {
		t.Errorf("id is %q, want %q", rows[1][0], "2")
	}
}

// A column under something nullable is nullable, however its own field was declared: the null
// it can show comes from above it.
func TestFlattenCarriesNullabilityDown(t *testing.T) {
	leaves := flattenSchema(arrow.NewSchema([]arrow.Field{
		{Name: "id", Type: arrow.PrimitiveTypes.Int64},
		{Name: "user", Type: arrow.StructOf(
			arrow.Field{Name: "tier", Type: arrow.BinaryTypes.String}, // not nullable itself
		), Nullable: true},
	}, nil))

	if len(leaves) != 2 {
		t.Fatalf("got %d leaves, want 2", len(leaves))
	}
	if leaves[0].Nullable {
		t.Error("id is reported nullable")
	}
	if !leaves[1].Nullable {
		t.Error("user.tier is not reported nullable, though the record above it is")
	}
}

// A struct with no fields has no leaves to stand in for it, so it stays a column of its own
// rather than disappearing from the table.
func TestFlattenKeepsAnEmptyStruct(t *testing.T) {
	leaves := flattenSchema(arrow.NewSchema([]arrow.Field{
		{Name: "nothing", Type: arrow.StructOf(), Nullable: true},
	}, nil))

	if len(leaves) != 1 || leaves[0].Name != "nothing" {
		t.Errorf("got %v, want the struct kept as one column", names(columnsOf(leaves)))
	}
}
