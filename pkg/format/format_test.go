// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFromKey(t *testing.T) {
	cases := []struct {
		key  string
		want Format
	}{
		{"data.parquet", Parquet},
		{"data.pq", Parquet},
		{"part-00000.snappy.parquet", Parquet},
		{"events.avro", Avro},
		{"events.ndjson", NDJSON},
		{"events.jsonl", NDJSON},
		{"config.json", JSON},

		// Case in a key is not meaningful to the format.
		{"DATA.PARQUET", Parquet},
		{"Events.Avro", Avro},

		// The extension of the object, not of a folder along the way.
		{"year=2024/parquet.d/blob", Unknown},

		{"archive.tar.gz", Unknown},
		{"image.png", Unknown},
		{"part-00000", Unknown},
		{"", Unknown},
	}

	for _, c := range cases {
		if got := FromKey(c.key); got != c.want {
			t.Errorf("FromKey(%q) = %v, want %v", c.key, got, c.want)
		}
	}
}

func TestVerifyAcceptsTheRealThing(t *testing.T) {
	cases := []struct {
		name   string
		format Format
		head   []byte
		tail   []byte
	}{
		{"parquet", Parquet, []byte("PAR1\x15\x04"), []byte("\x00\x00PAR1")},
		{"avro", Avro, []byte("Obj\x01\x04\x14"), nil},
		{"ndjson", NDJSON, []byte(`{"a":1}` + "\n"), nil},
		{"ndjson after whitespace", NDJSON, []byte("\n\t {\"a\":1}"), nil},
		{"json object", JSON, []byte(`{"a":1}`), nil},
		{"json array", JSON, []byte(`[1,2]`), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := Verify(c.format, c.head, c.tail); err != nil {
				t.Errorf("Verify: %v", err)
			}
		})
	}
}

func TestVerifyRejectsAMismatch(t *testing.T) {
	cases := []struct {
		name   string
		format Format
		head   []byte
		tail   []byte
	}{
		// The common case: something else entirely, named .parquet.
		{"not parquet at all", Parquet, []byte("\x89PNG\r\n"), []byte("IEND\xaeB`\x82")},
		// A parquet file whose write never finished: it opens right and ends wrong.
		{"truncated parquet", Parquet, []byte("PAR1\x15\x04"), []byte("\x00\x00\x00\x00")},
		{"shorter than the magic", Parquet, []byte("PA"), []byte("PA")},
		{"empty", Parquet, nil, nil},
		{"not avro", Avro, []byte("PAR1\x15\x04"), nil},
		{"ndjson holding text", NDJSON, []byte("id,name\n1,a\n"), nil},
		{"ndjson holding nothing", NDJSON, []byte("   \n\t "), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Verify(c.format, c.head, c.tail)
			if err == nil {
				t.Fatal("Verify accepted a file of another format")
			}
			// The message names the format asked for, since that is what the key claimed.
			if !strings.Contains(err.Error(), c.format.String()) {
				t.Errorf("error %q does not name %s", err, c.format)
			}
		})
	}
}

func TestVerifyRejectsUnknown(t *testing.T) {
	if err := Verify(Unknown, []byte("anything"), nil); err == nil {
		t.Error("Verify accepted an unknown format")
	}
}

func TestMagicReadsBothEnds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	body := []byte("PAR1" + strings.Repeat("x", 500) + "PAR1")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	head, tail, err := Magic(path)
	if err != nil {
		t.Fatalf("Magic: %v", err)
	}
	if string(head[:4]) != "PAR1" {
		t.Errorf("head = %q", head[:4])
	}
	if string(tail[len(tail)-4:]) != "PAR1" {
		t.Errorf("tail = %q", tail[len(tail)-4:])
	}
}

// A file shorter than the window Magic reads is returned whole rather than refused: it is
// Verify's business whether what little there is matches.
func TestMagicOnAShortFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, []byte("PA"), 0o644); err != nil {
		t.Fatal(err)
	}

	head, tail, err := Magic(path)
	if err != nil {
		t.Fatalf("Magic: %v", err)
	}
	if string(head) != "PA" || string(tail) != "PA" {
		t.Errorf("head = %q, tail = %q", head, tail)
	}
}

func TestMagicOnAnEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "f")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	head, tail, err := Magic(path)
	if err != nil {
		t.Fatalf("Magic: %v", err)
	}
	if len(head) != 0 || len(tail) != 0 {
		t.Errorf("head = %q, tail = %q", head, tail)
	}
}

// Only the formats with a reader may be offered; the rest must be refused before an object is
// downloaded rather than after.
func TestSupported(t *testing.T) {
	if !Supported(Parquet) {
		t.Error("parquet has a reader and is not reported as supported")
	}
	if Supported(Unknown) {
		t.Error("an unknown format is reported as supported")
	}
}

func TestFormatString(t *testing.T) {
	for _, f := range []Format{Unknown, Parquet, Avro, NDJSON, JSON} {
		if f.String() == "" {
			t.Errorf("format %d has no name", f)
		}
	}
}
