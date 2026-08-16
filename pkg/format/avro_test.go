// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/hamba/avro/v2/ocf"
)

// avroSchema covers what a reader has to survive: a plain field, a nullable one written as a
// union, a nested record and an array.
const avroSchema = `{
  "type": "record",
  "name": "Event",
  "fields": [
    {"name": "id", "type": "long"},
    {"name": "name", "type": ["null", "string"], "default": null},
    {"name": "user", "type": ["null", {
      "type": "record",
      "name": "User",
      "fields": [{"name": "tier", "type": "string"}]
    }], "default": null},
    {"name": "tags", "type": {"type": "array", "items": "string"}}
  ]
}`

type avroUser struct {
	Tier string `avro:"tier"`
}

type avroEvent struct {
	ID   int64     `avro:"id"`
	Name *string   `avro:"name"`
	User *avroUser `avro:"user"`
	Tags []string  `avro:"tags"`
}

// writeAvro writes an OCF of rows records. Every fifth record leaves the nullable fields unset,
// so nulls are exercised wherever a batch happens to fall.
func writeAvro(t *testing.T, name string, rows int, opts ...ocf.EncoderFunc) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	enc, err := ocf.NewEncoder(avroSchema, f, opts...)
	if err != nil {
		t.Fatalf("opening the encoder: %v", err)
	}

	for i := range rows {
		event := avroEvent{ID: int64(i), Tags: []string{"a", fmt.Sprintf("t%d", i%3)}}
		if i%5 != 4 {
			name := fmt.Sprintf("row-%d", i)
			event.Name = &name
			event.User = &avroUser{Tier: "gold"}
		}
		if err := enc.Encode(event); err != nil {
			t.Fatalf("encoding record %d: %v", i, err)
		}
	}

	if err := enc.Close(); err != nil {
		t.Fatalf("closing the encoder: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAvroIsWhatItSaysItIs(t *testing.T) {
	head, _, err := Magic(writeAvro(t, "e.avro", 3))
	if err != nil {
		t.Fatalf("Magic: %v", err)
	}
	if err := Verify(Avro, head, nil); err != nil {
		t.Errorf("an OCF failed verification: %v", err)
	}
}

// An OCF carries the schema its records were written against, so there are columns to name.
func TestAvroColumns(t *testing.T) {
	r, err := Open("e.avro", writeAvro(t, "e.avro", 3), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	if !Tabular(Avro) {
		t.Error("avro is not reported tabular")
	}

	// user is a record, so it is shown as its leaves rather than as a value of its own.
	cols := r.Columns()
	if len(cols) != 4 {
		t.Fatalf("got %d columns, want 4: %v", len(cols), names(cols))
	}
	for i, want := range []string{"id", "name", "user.tier", "tags"} {
		if cols[i].Name != want {
			t.Errorf("column %d is %q, want %q", i, cols[i].Name, want)
		}
		if cols[i].Type == "" {
			t.Errorf("column %q has no type", cols[i].Name)
		}
	}

	// A union of null and a type is what a nullable field is written as.
	if !cols[1].Nullable {
		t.Error("name is not reported nullable")
	}

	// An OCF has no count in front of its blocks.
	if got := r.NumRows(); got != -1 {
		t.Errorf("NumRows = %d, want -1", got)
	}
}

func TestAvroRows(t *testing.T) {
	r, err := Open("e.avro", writeAvro(t, "e.avro", 5), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	rows := drainRows(t, r)
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5", len(rows))
	}

	if rows[0][0] != "0" {
		t.Errorf("id = %q, want %q", rows[0][0], "0")
	}
	if !strings.Contains(rows[0][1], "row-0") {
		t.Errorf("name = %q, want it to hold row-0", rows[0][1])
	}
	// A nested field is its own column now, holding the value and not a JSON blob of it.
	if rows[0][2] != "gold" {
		t.Errorf("user.tier is %q, want %q", rows[0][2], "gold")
	}
	if !strings.Contains(rows[0][3], "a") {
		t.Errorf("array rendered as %q", rows[0][3])
	}

	// The fifth record left the nullable fields unset.
	if rows[4][1] != NullValue {
		t.Errorf("unset name rendered as %q, want %q", rows[4][1], NullValue)
	}
	// The record itself was unset, so every column under it reads as null.
	if rows[4][2] != NullValue {
		t.Errorf("user.tier under an unset record is %q, want %q", rows[4][2], NullValue)
	}
}

// Every record comes back exactly once, however the batches fall across the file's blocks.
func TestAvroBatchesCoverEveryRow(t *testing.T) {
	const rows = 25
	path := writeAvro(t, "e.avro", rows)

	for _, batchRows := range []int{2, 7, 25, 100} {
		r, err := Open("e.avro", path, batchRows)
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

		if len(ids) != rows {
			t.Fatalf("batchRows=%d: got %d rows, want %d", batchRows, len(ids), rows)
		}
		for i, id := range ids {
			if want := strconv.Itoa(i); id != want {
				t.Errorf("batchRows=%d: row %d has id %q, want %q", batchRows, i, id, want)
			}
		}
	}
}

// An OCF compresses its own blocks, with the codec named in its header. What the reader does
// with that is the codec's business, not skog's.
func TestAvroInternalCodec(t *testing.T) {
	for _, codec := range []ocf.CodecName{ocf.Null, ocf.Deflate, ocf.Snappy} {
		t.Run(string(codec), func(t *testing.T) {
			path := writeAvro(t, "e.avro", 6, ocf.WithCodec(codec))

			r, err := Open("e.avro", path, 10)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() { _ = r.Close() }()

			if rows := drainRows(t, r); len(rows) != 6 {
				t.Errorf("got %d rows, want 6", len(rows))
			}
		})
	}
}

// An OCF is a stream from its first byte, so a gzip wrapper over one reads as any other
// compressed object does.
func TestAvroGzipped(t *testing.T) {
	plain, err := os.ReadFile(writeAvro(t, "e.avro", 4))
	if err != nil {
		t.Fatal(err)
	}

	r, err := Open("e.avro.gz", writeGzip(t, "e.avro.gz", string(plain)), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	if rows := drainRows(t, r); len(rows) != 4 {
		t.Errorf("got %d rows, want 4", len(rows))
	}
}

// A file with the schema and no records is empty, not broken.
func TestAvroWithNoRecords(t *testing.T) {
	r, err := Open("e.avro", writeAvro(t, "e.avro", 0), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	if rows := drainRows(t, r); len(rows) != 0 {
		t.Errorf("got %d rows, want none", len(rows))
	}
}

func TestAvroRejectsSomethingElse(t *testing.T) {
	r, err := Open("e.avro", writeFile(t, "e.avro", "id,name\n1,ann\n"), 10)
	if err == nil {
		_ = r.Close()
		t.Fatal("Open accepted a file that is not an OCF")
	}
	if !strings.Contains(err.Error(), "bad magic") {
		t.Errorf("error does not name the problem: %v", err)
	}
}

func TestAvroIsSupported(t *testing.T) {
	if !Supported(Avro) {
		t.Error("avro has a reader and is not reported supported")
	}
}

func drainRows(t *testing.T, r Reader) [][]string {
	t.Helper()

	var rows [][]string
	for {
		batch, err := r.NextBatch()
		if err != nil {
			t.Fatalf("NextBatch: %v", err)
		}
		if batch == nil {
			return rows
		}
		rows = append(rows, batch...)
	}
}

func names(cols []Column) []string {
	out := make([]string, 0, len(cols))
	for _, c := range cols {
		out = append(out, c.Name)
	}
	return out
}

// The schema shown is the one out of the file's own header, not a description built from it.
func TestAvroSchemaIsTheWriterSchema(t *testing.T) {
	r, err := Open("e.avro", writeAvro(t, "e.avro", 3), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	got := r.Schema()
	if !json.Valid([]byte(got)) {
		t.Fatalf("schema is not valid JSON:\n%s", got)
	}
	// Laid out for reading rather than the one line the header holds.
	if !strings.Contains(got, "\n") {
		t.Errorf("schema was not laid out:\n%s", got)
	}

	// What the writer declared, down to the union that makes a field nullable.
	for _, want := range []string{`"name": "Event"`, `"id"`, `"long"`, `"null"`, `"User"`, `"tier"`} {
		if !strings.Contains(got, want) {
			t.Errorf("schema does not hold %s:\n%s", want, got)
		}
	}
}
