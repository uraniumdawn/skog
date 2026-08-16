// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeGzip writes content gzipped, as a lake writes it.
func writeGzip(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)

	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	if _, err := gz.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// linesOf drains a reader into the lines it holds.
func linesOf(t *testing.T, r Reader) []string {
	t.Helper()

	var lines []string
	for {
		batch, err := r.NextBatch()
		if err != nil {
			t.Fatalf("NextBatch: %v", err)
		}
		if batch == nil {
			return lines
		}
		for _, row := range batch {
			lines = append(lines, row[0])
		}
	}
}

const ndjsonSample = `{"id":1,"user":{"name":"ann","tier":"gold"},"tags":["a","b"]}
{"id":2,"user":{"name":"bo"},"tags":[],"retry":true}
{"id":3,"user":null}
`

// A line is shown as it was written. Nothing is parsed, so nothing is reordered, reformatted or
// dropped on the way to the screen.
func TestNDJSONLinesAreVerbatim(t *testing.T) {
	r, err := Open("e.jsonl", writeFile(t, "e.jsonl", ndjsonSample), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	batch, err := r.NextBatch()
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if len(batch) != 3 {
		t.Fatalf("got %d lines, want 3", len(batch))
	}

	want := strings.Split(strings.TrimRight(ndjsonSample, "\n"), "\n")
	for i, line := range want {
		if len(batch[i]) != 1 {
			t.Fatalf("line %d came back as %d cells, want 1", i, len(batch[i]))
		}
		if batch[i][0] != line {
			t.Errorf("line %d is\n  %q\nwant\n  %q", i, batch[i][0], line)
		}
	}
}

// A line-oriented file has no columns to put in a header.
func TestNDJSONHasNoColumns(t *testing.T) {
	r, err := Open("e.jsonl", writeFile(t, "e.jsonl", ndjsonSample), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	if cols := r.Columns(); len(cols) != 0 {
		t.Errorf("got %d columns, want none", len(cols))
	}
	if got := r.NumRows(); got != -1 {
		t.Errorf("NumRows = %d, want -1: a line count costs reading the whole file", got)
	}
	if Tabular(NDJSON) {
		t.Error("ndjson is reported tabular")
	}
	if !Tabular(Parquet) {
		t.Error("parquet is not reported tabular")
	}
}

func TestNDJSONBatching(t *testing.T) {
	var b strings.Builder
	for i := range 25 {
		fmt.Fprintf(&b, "{\"id\":%d}\n", i)
	}
	path := writeFile(t, "e.jsonl", b.String())

	for _, batchRows := range []int{1, 7, 25, 100} {
		r, err := Open("e.jsonl", path, batchRows)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}

		var lines []string
		for {
			batch, err := r.NextBatch()
			if err != nil {
				t.Fatalf("NextBatch: %v", err)
			}
			if batch == nil {
				break
			}
			if len(batch) > batchRows {
				t.Errorf("batch of %d, over the %d asked for", len(batch), batchRows)
			}
			for _, row := range batch {
				lines = append(lines, row[0])
			}
		}
		_ = r.Close()

		if len(lines) != 25 {
			t.Fatalf("batchRows=%d: got %d lines, want 25", batchRows, len(lines))
		}
		if lines[0] != `{"id":0}` || lines[24] != `{"id":24}` {
			t.Errorf("batchRows=%d: first %q, last %q", batchRows, lines[0], lines[24])
		}
	}
}

// A blank line is a gap in the file, not a record, and would show as an empty row.
func TestNDJSONSkipsBlankLines(t *testing.T) {
	r, err := Open("e.jsonl", writeFile(t, "e.jsonl", "{\"a\":1}\n\n   \n{\"a\":2}\n"), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	batch, err := r.NextBatch()
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if len(batch) != 2 {
		t.Fatalf("got %d lines, want 2", len(batch))
	}
}

// A single JSON document is laid out for reading, however it arrived: a minified one would
// otherwise be a single line as wide as the file.
func TestJSONDocumentIsPrettyPrinted(t *testing.T) {
	minified := `{"server":{"host":"a","port":8080},"rules":[1,2]}`

	r, err := Open("c.json", writeFile(t, "c.json", minified), 100)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	got := linesOf(t, r)
	want := []string{
		`{`,
		`  "server": {`,
		`    "host": "a",`,
		`    "port": 8080`,
		`  },`,
		`  "rules": [`,
		`    1,`,
		`    2`,
		`  ]`,
		`}`,
	}
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d:\n%s", len(got), len(want), strings.Join(got, "\n"))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d is %q, want %q", i, got[i], want[i])
		}
	}
}

// Indenting is a lexical transform, not a decode: what the writer wrote is what is shown. A
// decode would sort nothing but would put every number through a float, and 2026 written as
// 2.026e3 is not what is in the file.
func TestJSONDocumentKeepsKeyOrderAndNumbers(t *testing.T) {
	doc := `{"zeta":1,"alpha":2,"big":12345678901234567890,"exact":0.1000}`

	r, err := Open("c.json", writeFile(t, "c.json", doc), 100)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	joined := strings.Join(linesOf(t, r), "\n")
	if !strings.Contains(joined, `"zeta"`) ||
		strings.Index(joined, `"zeta"`) > strings.Index(joined, `"alpha"`) {
		t.Errorf("keys were reordered:\n%s", joined)
	}
	for _, number := range []string{"12345678901234567890", "0.1000"} {
		if !strings.Contains(joined, number) {
			t.Errorf("number %s did not survive:\n%s", number, joined)
		}
	}
}

// A .json holding records rather than one document is ndjson by another name. It will not
// indent, and is shown as it was written rather than refused.
func TestJSONFallsBackToVerbatim(t *testing.T) {
	r, err := Open("c.json", writeFile(t, "c.json", ndjsonSample), 100)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	got := linesOf(t, r)
	want := strings.Split(strings.TrimRight(ndjsonSample, "\n"), "\n")
	if len(got) != len(want) {
		t.Fatalf("got %d lines, want %d", len(got), len(want))
	}
	if got[0] != want[0] {
		t.Errorf("line 0 is %q, want %q", got[0], want[0])
	}
}

// NDJSON is not laid out: a record is shown as the writer left it, one line each.
func TestNDJSONIsNotPrettyPrinted(t *testing.T) {
	r, err := Open("e.jsonl", writeFile(t, "e.jsonl", ndjsonSample), 100)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	if got := linesOf(t, r); len(got) != 3 {
		t.Errorf("got %d lines, want 3 — the records were reformatted", len(got))
	}
}

func TestGzip(t *testing.T) {
	cases := []struct {
		name  string
		key   string
		body  string
		lines int
	}{
		{"ndjson", "events.jsonl.gz", ndjsonSample, 3},
		{"ndjson by the other name", "events.ndjson.gz", ndjsonSample, 3},
		// A document is indented after it is decompressed, as an uncompressed one is.
		{"json document", "c.json.gz", `{"a":{"b":1}}`, 5},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, err := Open(c.key, writeGzip(t, c.key, c.body), 100)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer func() { _ = r.Close() }()

			if got := linesOf(t, r); len(got) != c.lines {
				t.Errorf("got %d lines, want %d", len(got), c.lines)
			}
		})
	}
}

// The compression suffix is not the format: what is under it is.
func TestFromKeyIgnoresTheCompressionSuffix(t *testing.T) {
	cases := []struct {
		key        string
		want       Format
		compressed bool
	}{
		{"events.jsonl.gz", NDJSON, true},
		{"events.ndjson.gz", NDJSON, true},
		{"c.json.gz", JSON, true},
		{"EVENTS.JSONL.GZ", NDJSON, true},
		{"events.jsonl", NDJSON, false},

		// Nothing skog reads is under either of these.
		{"archive.tar.gz", Unknown, true},
		{"blob.gz", Unknown, true},

		// Recognised, and refused later: parquet compresses itself.
		{"data.parquet.gz", Parquet, true},
	}

	for _, c := range cases {
		if got := FromKey(c.key); got != c.want {
			t.Errorf("FromKey(%q) = %v, want %v", c.key, got, c.want)
		}
		if got := Compressed(c.key); got != c.compressed {
			t.Errorf("Compressed(%q) = %v, want %v", c.key, got, c.compressed)
		}
	}
}

// A gzipped parquet is refused outright rather than read as a broken parquet: parquet is
// compressed a column at a time and read by seeking to its footer, so a wrapper defeats both.
func TestGzippedParquetIsRefused(t *testing.T) {
	_, err := Open("data.parquet.gz", writeGzip(t, "data.parquet.gz", "PAR1"), 10)
	if err == nil {
		t.Fatal("Open accepted a gzipped parquet")
	}
	if !strings.Contains(err.Error(), ".gz") {
		t.Errorf("error does not name the problem: %v", err)
	}
}

func TestGzipRejectsSomethingElse(t *testing.T) {
	// The key says .gz; the bytes are the plain records.
	_, err := Open("events.jsonl.gz", writeFile(t, "events.jsonl.gz", ndjsonSample), 10)
	if err == nil {
		t.Fatal("Open accepted a file that is not gzipped")
	}
	if !strings.Contains(err.Error(), "gzip") {
		t.Errorf("error does not name the problem: %v", err)
	}
}

// The check is on the content, not on the gzip wrapper around it.
func TestGzipOfSomethingElseIsRejected(t *testing.T) {
	_, err := Open("events.jsonl.gz", writeGzip(t, "events.jsonl.gz", "id,name\n1,ann\n"), 10)
	if err == nil {
		t.Fatal("Open accepted gzipped csv as ndjson")
	}
	if !strings.Contains(err.Error(), "bad magic") {
		t.Errorf("error does not name the problem: %v", err)
	}
}

// An object with nothing in it is empty, not malformed.
func TestEmptyFileIsNoRows(t *testing.T) {
	for _, key := range []string{"e.jsonl", "c.json"} {
		r, err := Open(key, writeFile(t, key, ""), 10)
		if err != nil {
			t.Fatalf("Open(%s): %v", key, err)
		}
		if got := linesOf(t, r); len(got) != 0 {
			t.Errorf("%s: got %d lines, want none", key, len(got))
		}
		_ = r.Close()
	}
}

// A line far longer than bufio's own default must not end the read.
func TestNDJSONReadsAVeryLongLine(t *testing.T) {
	long := `{"blob":"` + strings.Repeat("x", 300_000) + `"}`
	r, err := Open("e.jsonl", writeFile(t, "e.jsonl", long+"\n"), 10)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = r.Close() }()

	batch, err := r.NextBatch()
	if err != nil {
		t.Fatalf("NextBatch: %v", err)
	}
	if len(batch) != 1 {
		t.Fatalf("got %d lines, want 1", len(batch))
	}
	if len(batch[0][0]) != len(long) {
		t.Errorf("line came back %d bytes, want %d", len(batch[0][0]), len(long))
	}
}

func TestNDJSONRejectsSomethingElse(t *testing.T) {
	r, err := Open("e.jsonl", writeFile(t, "e.jsonl", "id,name\n1,ann\n"), 10)
	if err == nil {
		_ = r.Close()
		t.Fatal("Open accepted a file that is not ndjson")
	}
	if !strings.Contains(err.Error(), "bad magic") {
		t.Errorf("error does not name the problem: %v", err)
	}
}

func TestNDJSONIsSupported(t *testing.T) {
	for _, f := range []Format{NDJSON, JSON} {
		if !Supported(f) {
			t.Errorf("%s has a reader and is not reported supported", f)
		}
	}
}
