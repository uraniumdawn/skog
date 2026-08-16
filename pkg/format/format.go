// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package format reads the data files an S3 bucket tends to hold — parquet today, Avro and
// NDJSON to follow — as rows of display strings.
//
// What leaves this package is text: column names, types, and cells. The columnar libraries
// behind it stay behind it, so the UI renders a table without knowing what wrote the file.
package format

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Format is the kind of data file an object holds.
type Format int

const (
	// Unknown is an object skog has no reader for. It is not downloaded.
	Unknown Format = iota
	Parquet
	Avro
	// NDJSON is one JSON value per line, the shape a data lake writes. It is a table.
	NDJSON
	// JSON is a single JSON document. It has no rows, and is shown as text rather than a table.
	JSON
)

// String names the format as an error message and the status line refer to it.
func (f Format) String() string {
	switch f {
	case Parquet:
		return "parquet"
	case Avro:
		return "avro"
	case NDJSON:
		return "ndjson"
	case JSON:
		return "json"
	default:
		return "unknown"
	}
}

// magicWindow is how much of each end of a file Verify is given. Four bytes is what the magic
// numbers need; the rest is room for a check that wants more.
const magicWindow = 16

// ErrUnsupported reports a format skog cannot read yet.
var ErrUnsupported = errors.New("no viewer for this format")

// extensions maps what a key ends in to what it holds. A key is only a claim — Verify is what
// checks it — but it is a claim that costs nothing to read, and it is what keeps skog from
// downloading an object it could not have shown.
var extensions = map[string]Format{
	".parquet": Parquet,
	".pq":      Parquet,
	".avro":    Avro,
	".ndjson":  NDJSON,
	".jsonl":   NDJSON,
	".json":    JSON,
}

// gzSuffix is the compression a key carries after the extension naming its format, as
// events.jsonl.gz does. A lake writes a great many of them.
const gzSuffix = ".gz"

// FromKey is the format the key claims, Unknown for one that claims nothing skog reads.
//
// A compression suffix is not the format: events.jsonl.gz holds ndjson, and what it takes to
// get at it is Compressed's business. Only the object's own extension counts otherwise — a key
// is a path, and a folder along the way may be named anything at all.
func FromKey(key string) Format {
	return extensions[strings.ToLower(filepath.Ext(uncompressedKey(key)))]
}

// Compressed reports whether the key names a gzipped object.
func Compressed(key string) bool {
	return strings.HasSuffix(strings.ToLower(key), gzSuffix)
}

// uncompressedKey is the key with its compression suffix taken off, so what is left ends in the
// extension naming the format.
func uncompressedKey(key string) string {
	lower := strings.ToLower(key)
	if !strings.HasSuffix(lower, gzSuffix) {
		return lower
	}
	return strings.TrimSuffix(lower, gzSuffix)
}

// Supported reports whether the format has a reader. A format may be recognised and still not
// be readable, in which case saying so before an object is fetched is the point.
func Supported(f Format) bool {
	switch f {
	case Parquet, Avro, NDJSON, JSON:
		return true
	default:
		return false
	}
}

// Verify checks the file's own bytes against the format its key claimed, given the leading and
// trailing bytes Magic reads.
//
// It is what turns "this is not what it says it is" into a message naming the problem, rather
// than into whatever a parser makes of the wrong bytes.
func Verify(f Format, head, tail []byte) error {
	switch f {
	case Parquet:
		// Parquet is bracketed by its magic. The tail is the half worth checking: a file that
		// opens with PAR1 and does not end with it is a write that never finished.
		if !starts(head, "PAR1") || !ends(tail, "PAR1") {
			return badMagic(f)
		}
	case Avro:
		if !starts(head, "Obj\x01") {
			return badMagic(f)
		}
	case NDJSON, JSON:
		// JSON has no magic number, so the most that can be said is that the first thing in
		// the file begins a JSON value.
		switch first(head) {
		case '{', '[':
		default:
			return badMagic(f)
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnsupported, f)
	}

	return nil
}

// Magic reads the leading and trailing bytes of a file, as much of each as Verify is given.
// A file shorter than that comes back whole, twice.
func Magic(path string) (head, tail []byte, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	size := info.Size()

	window := min(int64(magicWindow), size)

	head = make([]byte, window)
	if _, err := io.ReadFull(f, head); err != nil {
		return nil, nil, err
	}

	tail = make([]byte, window)
	if _, err := f.ReadAt(tail, size-window); err != nil {
		return nil, nil, err
	}

	return head, tail, nil
}

func badMagic(f Format) error {
	return fmt.Errorf("not a %s file (bad magic)", f)
}

func starts(b []byte, magic string) bool {
	return len(b) >= len(magic) && string(b[:len(magic)]) == magic
}

func ends(b []byte, magic string) bool {
	return len(b) >= len(magic) && string(b[len(b)-len(magic):]) == magic
}

// first is the first byte that is not whitespace, zero when there is none.
func first(b []byte) byte {
	for _, c := range b {
		switch c {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return c
		}
	}
	return 0
}
