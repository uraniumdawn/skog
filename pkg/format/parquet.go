// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package format

import (
	"context"
	"io"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/memory"
	"github.com/apache/arrow-go/v18/parquet/file"
	"github.com/apache/arrow-go/v18/parquet/pqarrow"
	"github.com/apache/arrow-go/v18/parquet/schema"
)

// parquetReader reads a parquet file as arrow records and hands them out in batches of a fixed
// size.
//
// A record is whatever the file's row groups and the batch size make it, so it does not line up
// with a batch: what a record has left over is held in pending and given out by the next call.
// Rows come out in file order either way.
type parquetReader struct {
	reader  *file.Reader
	records pqarrow.RecordReader

	// leaves are the flattened columns and the paths that reach them; columns is what a
	// caller sees of them.
	leaves    []leafColumn
	columns   []Column
	numRows   int64
	batchRows int

	// pending is the record a batch stopped part way through, and offset is how far into it
	// that was.
	pending arrow.RecordBatch
	offset  int64
}

// openParquet opens the file at path, which has already been checked to be one.
func openParquet(path string, batchRows int) (Reader, error) {
	// Not memory mapped: the file is in skog's cache, and a mapping would keep it in place
	// after eviction has removed it.
	reader, err := file.OpenParquetFile(path, false)
	if err != nil {
		return nil, err
	}

	// BatchSize bounds what one record holds, so no single read pulls a whole row group into
	// memory to show a screenful of it.
	props := pqarrow.ArrowReadProperties{BatchSize: int64(batchRows)}
	fileReader, err := pqarrow.NewFileReader(reader, props, memory.DefaultAllocator)
	if err != nil {
		return nil, closeAfter(reader, err)
	}

	schema, err := fileReader.Schema()
	if err != nil {
		return nil, closeAfter(reader, err)
	}

	// Every column, every row group: which of them to read is the reader's business only once
	// there is a column picker and a row-group jump to ask for less.
	records, err := fileReader.GetRecordReader(context.Background(), nil, nil)
	if err != nil {
		return nil, closeAfter(reader, err)
	}

	leaves := flattenSchema(schema)

	return &parquetReader{
		reader:    reader,
		records:   records,
		leaves:    leaves,
		columns:   columnsOf(leaves),
		numRows:   reader.NumRows(),
		batchRows: batchRows,
	}, nil
}

func (r *parquetReader) Columns() []Column { return r.columns }

func (r *parquetReader) NumRows() int64 { return r.numRows }

// Schema renders the tree in the file's footer as JSON.
//
// Parquet has no schema document of its own to hand back the way Avro does — what it stores is
// a tree of nodes — so this is a rendering of it rather than a copy. What the tree says is kept:
// the physical type each column is stored as, the logical type it stands for, and whether it is
// required, optional or repeated, which is what tells a nullable column from a list.
func (r *parquetReader) Schema() string {
	root := r.reader.MetaData().Schema.Root()

	fields := make([]parquetField, 0, root.NumFields())
	for i := range root.NumFields() {
		fields = append(fields, parquetFieldOf(root.Field(i)))
	}

	return marshalJSON(parquetField{Name: root.Name(), Fields: fields})
}

// parquetField is one node of a parquet schema as it is shown.
type parquetField struct {
	Name string `json:"name"`
	// Repetition is required, optional or repeated. A repeated node is the list or map
	// wrapper, and an optional one is what makes a column nullable.
	Repetition string `json:"repetition,omitempty"`
	// Type is what the column is stored as on disk, absent on a group.
	Type string `json:"type,omitempty"`
	// LogicalType is what those bytes stand for — a string, a date, a decimal — absent when
	// the physical type is the whole story.
	LogicalType string         `json:"logicalType,omitempty"`
	Fields      []parquetField `json:"fields,omitempty"`
}

func parquetFieldOf(node schema.Node) parquetField {
	field := parquetField{
		Name:       node.Name(),
		Repetition: node.RepetitionType().String(),
	}

	// A logical type of "None" is a column that is only its physical type.
	if logical := node.LogicalType(); logical != nil && !logical.IsNone() {
		field.LogicalType = logical.String()
	}

	switch n := node.(type) {
	case *schema.PrimitiveNode:
		field.Type = n.PhysicalType().String()
	case *schema.GroupNode:
		field.Fields = make([]parquetField, 0, n.NumFields())
		for i := range n.NumFields() {
			field.Fields = append(field.Fields, parquetFieldOf(n.Field(i)))
		}
	}

	return field
}

func (r *parquetReader) NextBatch() ([][]string, error) {
	rows := make([][]string, 0, r.batchRows)

	for len(rows) < r.batchRows {
		if r.pending == nil {
			record, err := r.records.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, err
			}
			if record.NumRows() == 0 {
				continue
			}
			// The reader owns what it returns only until the next Read.
			record.Retain()
			r.pending = record
			r.offset = 0
		}

		take := min(int64(r.batchRows-len(rows)), r.pending.NumRows()-r.offset)
		slice := r.pending.NewSlice(r.offset, r.offset+take)
		rows = append(rows, cells(slice, r.leaves)...)
		slice.Release()

		r.offset += take
		if r.offset >= r.pending.NumRows() {
			r.pending.Release()
			r.pending = nil
		}
	}

	// The end of the file is no rows rather than an empty batch, so a caller can stop on it.
	if len(rows) == 0 {
		return nil, nil
	}
	return rows, nil
}

func (r *parquetReader) Close() error {
	if r.pending != nil {
		r.pending.Release()
		r.pending = nil
	}
	if r.records != nil {
		r.records.Release()
		r.records = nil
	}
	return r.reader.Close()
}

// closeAfter returns err, having closed the file it was opening. A half-built reader has no
// Close of its own to call.
func closeAfter(reader *file.Reader, err error) error {
	_ = reader.Close()
	return err
}
