// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"strings"
	"testing"
)

// v2Metadata is a format version 2 document: schemas named by id, partition specs named by id,
// snapshots pointing at manifest lists.
const v2Metadata = `{
  "format-version": 2,
  "table-uuid": "9d0c1e2a-0000-4000-8000-000000000001",
  "location": "s3://warehouse/db/orders",
  "last-updated-ms": 1755342120000,
  "last-sequence-number": 12,
  "current-snapshot-id": 8104928,
  "current-schema-id": 3,
  "schemas": [
    {"schema-id": 0, "fields": [{"id": 1, "name": "order_id", "required": true, "type": "long"}]},
    {"schema-id": 3, "fields": [
      {"id": 1, "name": "order_id", "required": true, "type": "long"},
      {"id": 2, "name": "ts", "required": true, "type": "timestamptz"},
      {"id": 3, "name": "amount", "required": false, "type": "decimal(12,2)"}
    ]}
  ],
  "default-spec-id": 1,
  "partition-specs": [
    {"spec-id": 0, "fields": []},
    {"spec-id": 1, "fields": [
      {"source-id": 2, "field-id": 1000, "name": "dt", "transform": "day"}
    ]}
  ],
  "default-sort-order-id": 1,
  "sort-orders": [
    {"order-id": 0, "fields": []},
    {"order-id": 1, "fields": [
      {"source-id": 2, "transform": "identity", "direction": "desc", "null-order": "nulls-last"}
    ]}
  ],
  "properties": {"write.format.default": "parquet"},
  "refs": {
    "main": {"snapshot-id": 8104928, "type": "branch"},
    "audit": {"snapshot-id": 7719301, "type": "tag"}
  },
  "snapshots": [
    {
      "snapshot-id": 7719301,
      "sequence-number": 11,
      "timestamp-ms": 1755291000000,
      "manifest-list": "s3://warehouse/db/orders/metadata/snap-7719301.avro",
      "summary": {"operation": "delete", "deleted-records": "4021"}
    },
    {
      "snapshot-id": 8104928,
      "parent-snapshot-id": 7719301,
      "sequence-number": 12,
      "timestamp-ms": 1755342120000,
      "schema-id": 3,
      "manifest-list": "s3://warehouse/db/orders/metadata/snap-8104928.avro",
      "summary": {"operation": "append", "added-records": "120400", "total-records": "980112"}
    }
  ]
}`

// v1Metadata is a format version 1 document: one schema, a top-level partition spec, and a
// snapshot listing its manifests outright.
const v1Metadata = `{
  "format-version": 1,
  "table-uuid": "9d0c1e2a-0000-4000-8000-000000000002",
  "location": "s3://warehouse/db/legacy",
  "last-updated-ms": 1600000000000,
  "current-snapshot-id": 42,
  "schema": {"type": "struct", "fields": [
    {"id": 1, "name": "id", "required": true, "type": "long"}
  ]},
  "partition-spec": [
    {"source-id": 1, "field-id": 1000, "name": "id_bucket", "transform": "bucket[16]"}
  ],
  "snapshots": [
    {
      "snapshot-id": 42,
      "timestamp-ms": 1600000000000,
      "manifests": [
        "s3://warehouse/db/legacy/metadata/m0.avro",
        "s3://warehouse/db/legacy/metadata/m1.avro"
      ],
      "summary": {"operation": "append"}
    }
  ]
}`

func TestParseMetadataV2(t *testing.T) {
	metadata, err := ParseMetadata(strings.NewReader(v2Metadata))
	if err != nil {
		t.Fatalf("ParseMetadata: %v", err)
	}

	if metadata.FormatVersion != 2 {
		t.Errorf("FormatVersion = %d, want 2", metadata.FormatVersion)
	}
	if metadata.Location != "s3://warehouse/db/orders" {
		t.Errorf("Location = %q", metadata.Location)
	}

	schema := metadata.CurrentSchema()
	if schema == nil {
		t.Fatal("CurrentSchema = nil")
	}
	if schema.SchemaID != 3 || len(schema.Fields) != 3 {
		t.Errorf("CurrentSchema = id %d with %d fields, want id 3 with 3", schema.SchemaID, len(schema.Fields))
	}
	if got := TypeString(schema.Fields[2].Type); got != "decimal(12,2)" {
		t.Errorf("amount type = %q, want decimal(12,2)", got)
	}

	spec := metadata.DefaultPartitionSpec()
	if spec == nil || spec.SpecID != 1 || len(spec.Fields) != 1 {
		t.Fatalf("DefaultPartitionSpec = %+v, want spec 1 with one field", spec)
	}
	if spec.Fields[0].Transform != "day" {
		t.Errorf("partition transform = %q, want day", spec.Fields[0].Transform)
	}

	order := metadata.DefaultSortOrder()
	if order == nil || order.OrderID != 1 || len(order.Fields) != 1 {
		t.Fatalf("DefaultSortOrder = %+v, want order 1 with one field", order)
	}

	if !metadata.IsCurrent(8104928) || metadata.IsCurrent(7719301) {
		t.Error("IsCurrent does not follow current-snapshot-id")
	}

	snapshot := metadata.SnapshotByID(8104928)
	if snapshot == nil {
		t.Fatal("SnapshotByID(8104928) = nil")
	}
	if snapshot.Operation() != "append" {
		t.Errorf("Operation = %q, want append", snapshot.Operation())
	}
	if snapshot.Committed().UnixMilli() != 1755342120000 {
		t.Errorf("Committed = %v", snapshot.Committed())
	}

	if refs := metadata.RefsAt(8104928); len(refs) != 1 || refs[0] != "main" {
		t.Errorf("RefsAt(8104928) = %v, want [main]", refs)
	}
	if refs := metadata.RefsAt(7719301); len(refs) != 1 || refs[0] != "audit" {
		t.Errorf("RefsAt(7719301) = %v, want [audit]", refs)
	}
}

func TestParseMetadataV1(t *testing.T) {
	metadata, err := ParseMetadata(strings.NewReader(v1Metadata))
	if err != nil {
		t.Fatalf("ParseMetadata: %v", err)
	}

	// Version 1 names neither a current schema nor a default spec; the single ones it writes
	// are what a reader gets.
	schema := metadata.CurrentSchema()
	if schema == nil || len(schema.Fields) != 1 || schema.Fields[0].Name != "id" {
		t.Fatalf("CurrentSchema = %+v, want the single v1 schema", schema)
	}

	spec := metadata.DefaultPartitionSpec()
	if spec == nil || len(spec.Fields) != 1 || spec.Fields[0].Name != "id_bucket" {
		t.Fatalf("DefaultPartitionSpec = %+v, want the top-level v1 spec", spec)
	}

	if metadata.DefaultSortOrder() != nil {
		t.Error("DefaultSortOrder should be nil for a document that declares none")
	}

	// A v1 snapshot may list its manifests instead of pointing at a manifest list.
	snapshot := metadata.SnapshotByID(42)
	if snapshot == nil {
		t.Fatal("SnapshotByID(42) = nil")
	}
	if snapshot.ManifestList != "" {
		t.Errorf("ManifestList = %q, want empty", snapshot.ManifestList)
	}
	if len(snapshot.Manifests) != 2 {
		t.Errorf("Manifests = %v, want two paths", snapshot.Manifests)
	}
}

func TestParseMetadataRejectsSomethingElse(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"not json", "PAR1"},
		{"json without a format version", `{"location":"s3://w/t"}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := ParseMetadata(strings.NewReader(test.body)); err == nil {
				t.Error("ParseMetadata accepted a document that is not table metadata")
			}
		})
	}
}
