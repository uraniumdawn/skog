// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package iceberg reads the metadata of an Apache Iceberg table out of S3: the table metadata
// documents, the snapshots they list, the manifests of a snapshot and the data files of a
// manifest.
//
// It knows nothing of the terminal and nothing of the AWS SDK — what it needs of S3 is the
// Source interface — and it reads only. What leaves the package is the metadata as the table
// declares it, with the differences between format versions 1, 2 and 3 already absorbed.
package iceberg

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/uraniumdawn/skog/pkg/util"
)

// Metadata is a table metadata document, as much of one as skog shows.
//
// The fields that format version 1 spells differently are held twice — Schema next to Schemas,
// PartitionSpec next to PartitionSpecs — and the accessors are what pick between them, so a
// caller never has to ask which version it is reading.
type Metadata struct {
	FormatVersion      int               `json:"format-version"`
	TableUUID          string            `json:"table-uuid"`
	Location           string            `json:"location"`
	LastUpdatedMS      int64             `json:"last-updated-ms"`
	LastColumnID       int               `json:"last-column-id"`
	CurrentSnapshotID  *int64            `json:"current-snapshot-id"`
	LastSequenceNumber int64             `json:"last-sequence-number"`
	Properties         map[string]string `json:"properties"`

	// v2 and up: the schemas of the table and which of them is current.
	Schemas         []Schema `json:"schemas"`
	CurrentSchemaID *int     `json:"current-schema-id"`
	// v1: the one schema the table was written with.
	Schema *Schema `json:"schema"`

	// v2 and up: the partition specs and which of them new data is written under.
	PartitionSpecs []PartitionSpec `json:"partition-specs"`
	DefaultSpecID  *int            `json:"default-spec-id"`
	// v1: the fields of the one spec.
	PartitionSpec []PartitionField `json:"partition-spec"`

	SortOrders         []SortOrder `json:"sort-orders"`
	DefaultSortOrderID *int        `json:"default-sort-order-id"`

	Snapshots []Snapshot     `json:"snapshots"`
	Refs      map[string]Ref `json:"refs"`
}

// Schema is one schema of the table.
type Schema struct {
	SchemaID int     `json:"schema-id"`
	Fields   []Field `json:"fields"`
}

// Field is one column of a schema. Type is the raw document: a primitive is a string, a struct,
// list or map is an object. TypeString is what renders either.
type Field struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Required bool   `json:"required"`
	Type     any    `json:"type"`
	Doc      string `json:"doc"`
}

// PartitionSpec is one partitioning of the table.
type PartitionSpec struct {
	SpecID int              `json:"spec-id"`
	Fields []PartitionField `json:"fields"`
}

// PartitionField is one partition column: a transform applied to a column of the schema.
type PartitionField struct {
	SourceID  int    `json:"source-id"`
	FieldID   int    `json:"field-id"`
	Name      string `json:"name"`
	Transform string `json:"transform"`
}

// SortOrder is one sort order of the table.
type SortOrder struct {
	OrderID int         `json:"order-id"`
	Fields  []SortField `json:"fields"`
}

// SortField is one term of a sort order.
type SortField struct {
	SourceID  int    `json:"source-id"`
	Transform string `json:"transform"`
	Direction string `json:"direction"`
	NullOrder string `json:"null-order"`
}

// Snapshot is one committed state of the table.
type Snapshot struct {
	SnapshotID       int64             `json:"snapshot-id"`
	ParentSnapshotID *int64            `json:"parent-snapshot-id"`
	SequenceNumber   int64             `json:"sequence-number"`
	TimestampMS      int64             `json:"timestamp-ms"`
	SchemaID         *int              `json:"schema-id"`
	Summary          map[string]string `json:"summary"`
	// ManifestList is where the snapshot's manifests are listed, and is how every v2 snapshot
	// records them.
	ManifestList string `json:"manifest-list"`
	// Manifests is how a version 1 snapshot may record them instead: the paths outright, with
	// no list file and so no counts to go with them.
	Manifests []string `json:"manifests"`
}

// Ref is a named pointer at a snapshot: a branch or a tag.
type Ref struct {
	SnapshotID int64  `json:"snapshot-id"`
	Type       string `json:"type"`
}

// ParseMetadata reads a table metadata document.
func ParseMetadata(r io.Reader) (*Metadata, error) {
	var metadata Metadata
	if err := json.NewDecoder(r).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("reading table metadata: %w", err)
	}
	if metadata.FormatVersion == 0 {
		return nil, fmt.Errorf("not an iceberg table metadata document: no format-version")
	}
	return &metadata, nil
}

// LastUpdated is when the table was last committed to.
func (m *Metadata) LastUpdated() time.Time {
	return millis(m.LastUpdatedMS)
}

// CurrentSchema is the schema the table is read under, nil for a document that declares none.
//
// Version 1 wrote a single schema; version 2 writes them all and names the current one. A
// version 1 document may carry both, in which case the named one wins — that is the form a
// writer produces while migrating the table forward.
func (m *Metadata) CurrentSchema() *Schema {
	if m.CurrentSchemaID != nil {
		for i := range m.Schemas {
			if m.Schemas[i].SchemaID == *m.CurrentSchemaID {
				return &m.Schemas[i]
			}
		}
	}
	if m.Schema != nil {
		return m.Schema
	}
	if len(m.Schemas) > 0 {
		return &m.Schemas[0]
	}
	return nil
}

// DefaultPartitionSpec is the spec new data is written under, nil for an unpartitioned table.
// It picks between the version 1 and version 2 spellings the way CurrentSchema does.
func (m *Metadata) DefaultPartitionSpec() *PartitionSpec {
	if m.DefaultSpecID != nil {
		for i := range m.PartitionSpecs {
			if m.PartitionSpecs[i].SpecID == *m.DefaultSpecID {
				return &m.PartitionSpecs[i]
			}
		}
	}
	if len(m.PartitionSpec) > 0 {
		return &PartitionSpec{Fields: m.PartitionSpec}
	}
	if len(m.PartitionSpecs) > 0 {
		return &m.PartitionSpecs[0]
	}
	return nil
}

// DefaultSortOrder is the order new data is written in, nil for an unsorted table.
func (m *Metadata) DefaultSortOrder() *SortOrder {
	if m.DefaultSortOrderID == nil {
		return nil
	}
	for i := range m.SortOrders {
		if m.SortOrders[i].OrderID == *m.DefaultSortOrderID {
			return &m.SortOrders[i]
		}
	}
	return nil
}

// SnapshotByID returns the snapshot with the given id, nil when the table lists none.
func (m *Metadata) SnapshotByID(id int64) *Snapshot {
	for i := range m.Snapshots {
		if m.Snapshots[i].SnapshotID == id {
			return &m.Snapshots[i]
		}
	}
	return nil
}

// IsCurrent reports whether the given snapshot is the one the table currently points at.
func (m *Metadata) IsCurrent(id int64) bool {
	return m.CurrentSnapshotID != nil && *m.CurrentSnapshotID == id
}

// RefsAt names the branches and tags pointing at a snapshot, in name order.
func (m *Metadata) RefsAt(id int64) []string {
	var names []string
	for _, name := range util.SortedKeys(m.Refs) {
		if m.Refs[name].SnapshotID == id {
			names = append(names, name)
		}
	}
	return names
}

// Committed is when the snapshot was committed.
func (s *Snapshot) Committed() time.Time {
	return millis(s.TimestampMS)
}

// Operation is what the snapshot did — append, overwrite, delete, replace — as its own summary
// records it, empty for a snapshot that carries no summary.
func (s *Snapshot) Operation() string {
	return s.Summary["operation"]
}

// millis turns a millisecond timestamp into a time, the zero time for an absent one.
func millis(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}
