// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"errors"
	"io"

	"github.com/apache/arrow-go/v18/arrow/avro"
)

// avroReader reads an Avro object container file as arrow records.
//
// An OCF carries the schema its records were written against, so unlike NDJSON there are
// columns to put in a header — and unlike parquet there is no footer, so the file is read
// forwards from the first block and cannot say how many records it holds until it is done.
type avroReader struct {
	reader  *avro.OCFReader
	closers []io.Closer
	// leaves are the flattened columns and the paths that reach them; columns is what a caller
	// sees of them.
	leaves  []leafColumn
	columns []Column
}

// openAvro opens the file at path as an OCF, decompressing it on the way when the object is
// gzipped.
//
// An OCF compresses its own blocks, with the codec named in its header, and that is what a
// lake writes. A gzip wrapper over one is unusual but costs nothing to read: the format is a
// stream from its first byte, so nothing has to seek back through the decompression.
func openAvro(path string, compressed bool, batchRows int) (Reader, error) {
	buffered, closers, err := openStream(path, compressed)
	if err != nil {
		return nil, err
	}

	// The header is checked before the decoder is pointed at it, so something else named .avro
	// is a message naming the problem rather than whatever the decoder makes of the bytes.
	if head, _ := buffered.Peek(magicWindow); len(head) > 0 {
		if err := Verify(Avro, head, nil); err != nil {
			_ = closeAll(closers)
			return nil, err
		}
	}

	// WithChunk hands back batchRows records at a time, which is a page of the table. Its
	// other settings are of no use here: one record per row would be a batch per row, and
	// loading the file whole is the one thing a viewer opening it lazily must not do.
	reader, err := avro.NewOCFReader(buffered, avro.WithChunk(batchRows))
	if err != nil {
		_ = closeAll(closers)
		return nil, err
	}

	leaves := flattenSchema(reader.Schema())

	return &avroReader{
		reader:  reader,
		closers: closers,
		leaves:  leaves,
		columns: columnsOf(leaves),
	}, nil
}

func (r *avroReader) Columns() []Column { return r.columns }

// NumRows is unknown. An OCF is a run of blocks with no count in front of them, so how many
// records it holds is known only once every block has been read.
func (r *avroReader) NumRows() int64 { return -1 }

// Schema is the writer schema out of the file's own header, laid out for reading.
//
// It is Avro's own schema rather than a description built from the columns — the unions, the
// nested record names and the array item types are all as the file declares them. It is not
// byte-for-byte the header, though: it comes back through the decoder, which normalises it and
// drops what it does not carry forward, a field's default among it.
func (r *avroReader) Schema() string {
	return prettyJSON(r.reader.AvroSchema())
}

func (r *avroReader) NextBatch() ([][]string, error) {
	for r.reader.Next() {
		record := r.reader.RecordBatch()
		// A block can decode to nothing without the file being over.
		if record == nil || record.NumRows() == 0 {
			continue
		}
		return cells(record, r.leaves), nil
	}

	// The end of the file arrives as an EOF, which is how it ends rather than a failure.
	if err := r.reader.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return nil, nil
}

func (r *avroReader) Close() error {
	r.reader.Close()
	return closeAll(r.closers)
}
