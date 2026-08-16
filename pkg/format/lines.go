// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

// maxLineBytes is the longest line the reader will take. A record in a data lake can be very
// long — an embedded blob, a deeply nested document — and bufio's own default would end the
// read part way through the file rather than show it.
const maxLineBytes = 8 << 20

// indent is what one level of nesting is shown as in a document laid out for reading.
const indent = "  "

// linesReader hands out the lines of a stream, one batch at a time.
//
// What the lines are depends on the format. NDJSON is shown as it was written: the record as
// the writer left it is what tells you whether the writer was right, and any table built from
// it would reorder its keys, reformat its values and hide the ones the first batch happened
// not to mention. A single JSON document has no records to keep faith with, and is laid out
// for reading instead.
type linesReader struct {
	// closers are the file and, for a compressed object, the gzip reader over it. A document
	// read into memory has none: it is closed as soon as it has been read.
	closers   []io.Closer
	scanner   *bufio.Scanner
	batchRows int
}

func newLinesReader(src io.Reader, closers []io.Closer, batchRows int) *linesReader {
	scanner := bufio.NewScanner(src)
	scanner.Buffer(make([]byte, 0, bufio.MaxScanTokenSize), maxLineBytes)
	return &linesReader{closers: closers, scanner: scanner, batchRows: batchRows}
}

// openLines opens the file at path as lines of the given format, decompressing it on the way
// when the object is gzipped.
func openLines(f Format, path string, compressed bool, batchRows int) (Reader, error) {
	buffered, closers, err := openStream(path, compressed)
	if err != nil {
		return nil, err
	}

	// The format is checked against the front of the content, which for a compressed object
	// exists only once it has been decompressed this far. An object with no content at all is
	// empty rather than malformed, and shows as no rows.
	if head, _ := buffered.Peek(magicWindow); len(head) > 0 {
		if err := Verify(f, head, nil); err != nil {
			_ = closeAll(closers)
			return nil, err
		}
	}

	if f == JSON {
		// A document is read whole, so nothing is left to hold open once it has been.
		defer func() { _ = closeAll(closers) }()
		return openDocument(buffered, batchRows)
	}

	return newLinesReader(buffered, closers, batchRows), nil
}

// openStream opens the file at path for reading, through gzip when the object is compressed,
// and returns a buffered reader over the content with what has to be closed after it.
func openStream(path string, compressed bool) (*bufio.Reader, []io.Closer, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	closers := []io.Closer{file}

	var source io.Reader = file
	if compressed {
		// NewReader reads the header, so a key claiming .gz over something else fails here
		// rather than as unreadable lines further on.
		gz, err := gzip.NewReader(file)
		if err != nil {
			_ = closeAll(closers)
			return nil, nil, fmt.Errorf("not a gzip file: %w", err)
		}
		closers = append(closers, gz)
		source = gz
	}

	return bufio.NewReader(source), closers, nil
}

// openDocument reads a single JSON document and lays it out for reading, a field to a line.
//
// The document is indented rather than decoded and re-encoded, so what comes out is what went
// in: the keys keep the order the writer wrote them in, and numbers keep the form they were
// written in rather than passing through a float.
func openDocument(src io.Reader, batchRows int) (Reader, error) {
	raw, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", indent); err != nil {
		// Not one document after all — most often records concatenated into a .json, which is
		// ndjson by another name. A viewer's job is to show what is there, so it is shown as
		// it was written; that it is not laid out is the sign that it did not parse.
		return newLinesReader(bytes.NewReader(raw), nil, batchRows), nil
	}

	return newLinesReader(bytes.NewReader(pretty.Bytes()), nil, batchRows), nil
}

// Columns is none: a line is the whole record, and there is nothing to put in a header.
func (r *linesReader) Columns() []Column { return nil }

// NumRows is unknown. Counting the lines of a file means reading all of it, which is the one
// thing a viewer that opens a file lazily must not do.
func (r *linesReader) NumRows() int64 { return -1 }

func (r *linesReader) NextBatch() ([][]string, error) {
	rows := make([][]string, 0, r.batchRows)

	for len(rows) < r.batchRows && r.scanner.Scan() {
		line := r.scanner.Text()
		// A blank line is a gap in the file rather than a record, and would show as an empty
		// row. Everything else is kept exactly as it was written, whitespace included.
		if strings.TrimSpace(line) == "" {
			continue
		}
		rows = append(rows, []string{line})
	}

	if err := r.scanner.Err(); err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	return rows, nil
}

func (r *linesReader) Close() error {
	return closeAll(r.closers)
}

// closeAll closes what was opened, innermost first, and reports the first failure. Everything
// is closed whether or not one of them fails.
func closeAll(closers []io.Closer) error {
	var first error
	for i := len(closers) - 1; i >= 0; i-- {
		if err := closers[i].Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Schema is none. A line-oriented file declares nothing about its records: what shape they
// take is whatever each line happens to hold.
func (r *linesReader) Schema() string { return "" }
