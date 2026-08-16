// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"bytes"
	"testing"

	"github.com/hamba/avro/v2/ocf"
)

// manifestListV2Schema is the manifest list of format version 2, cut down to the fields skog
// reads. Its field names are the version 2 spelling: added_files_count and its siblings.
const manifestListV2Schema = `{
  "type": "record", "name": "manifest_file", "fields": [
    {"name": "manifest_path", "type": "string"},
    {"name": "manifest_length", "type": "long"},
    {"name": "partition_spec_id", "type": "int"},
    {"name": "content", "type": "int"},
    {"name": "sequence_number", "type": "long"},
    {"name": "min_sequence_number", "type": "long"},
    {"name": "added_snapshot_id", "type": "long"},
    {"name": "added_files_count", "type": "int"},
    {"name": "existing_files_count", "type": "int"},
    {"name": "deleted_files_count", "type": "int"},
    {"name": "added_rows_count", "type": "long"},
    {"name": "existing_rows_count", "type": "long"},
    {"name": "deleted_rows_count", "type": "long"}
  ]}`

// manifestListV1Schema is the same list as version 1 wrote it: no content, no sequence numbers,
// and the counts spelled added_data_files_count.
const manifestListV1Schema = `{
  "type": "record", "name": "manifest_file", "fields": [
    {"name": "manifest_path", "type": "string"},
    {"name": "manifest_length", "type": "long"},
    {"name": "partition_spec_id", "type": "int"},
    {"name": "added_snapshot_id", "type": "long"},
    {"name": "added_data_files_count", "type": ["null", "int"], "default": null},
    {"name": "existing_data_files_count", "type": ["null", "int"], "default": null},
    {"name": "deleted_data_files_count", "type": ["null", "int"], "default": null},
    {"name": "added_rows_count", "type": ["null", "long"], "default": null},
    {"name": "existing_rows_count", "type": ["null", "long"], "default": null},
    {"name": "deleted_rows_count", "type": ["null", "long"], "default": null}
  ]}`

// manifestV2Schema is a manifest of format version 2: entries carry a sequence number that may
// be null, and a data file carries a content field.
const manifestV2Schema = `{
  "type": "record", "name": "manifest_entry", "fields": [
    {"name": "status", "type": "int"},
    {"name": "snapshot_id", "type": ["null", "long"], "default": null},
    {"name": "sequence_number", "type": ["null", "long"], "default": null},
    {"name": "data_file", "type": {
      "type": "record", "name": "r2", "fields": [
        {"name": "content", "type": "int"},
        {"name": "file_path", "type": "string"},
        {"name": "file_format", "type": "string"},
        {"name": "partition", "type": {
          "type": "record", "name": "r102", "fields": [
            {"name": "dt", "type": ["null", "int"], "default": null}
          ]}},
        {"name": "record_count", "type": "long"},
        {"name": "file_size_in_bytes", "type": "long"}
      ]}}
  ]}`

// manifestV1Schema is a manifest of format version 1: no sequence numbers, and a data file with
// no content field, since version 1 has no delete files.
const manifestV1Schema = `{
  "type": "record", "name": "manifest_entry", "fields": [
    {"name": "status", "type": "int"},
    {"name": "snapshot_id", "type": ["null", "long"], "default": null},
    {"name": "data_file", "type": {
      "type": "record", "name": "r2", "fields": [
        {"name": "file_path", "type": "string"},
        {"name": "file_format", "type": "string"},
        {"name": "partition", "type": {
          "type": "record", "name": "r102", "fields": [
            {"name": "id_bucket", "type": ["null", "int"], "default": null}
          ]}},
        {"name": "record_count", "type": "long"},
        {"name": "file_size_in_bytes", "type": "long"}
      ]}}
  ]}`

// writeOCF encodes records into an avro object container file, with the key-value metadata a
// manifest carries.
func writeOCF(t *testing.T, schema string, metadata map[string][]byte, records ...any) []byte {
	t.Helper()

	var buf bytes.Buffer
	options := []ocf.EncoderFunc{ocf.WithCodec(ocf.Deflate)}
	if metadata != nil {
		options = append(options, ocf.WithMetadata(metadata))
	}

	encoder, err := ocf.NewEncoder(schema, &buf, options...)
	if err != nil {
		t.Fatalf("building the encoder: %v", err)
	}
	for _, record := range records {
		if err := encoder.Encode(record); err != nil {
			t.Fatalf("encoding a record: %v", err)
		}
	}
	if err := encoder.Close(); err != nil {
		t.Fatalf("closing the encoder: %v", err)
	}
	return buf.Bytes()
}

func TestReadManifestListV2(t *testing.T) {
	body := writeOCF(t, manifestListV2Schema, nil, map[string]any{
		"manifest_path":        "s3://warehouse/db/orders/metadata/abc-m0.avro",
		"manifest_length":      int64(8123),
		"partition_spec_id":    1,
		"content":              ContentData,
		"sequence_number":      int64(12),
		"min_sequence_number":  int64(11),
		"added_snapshot_id":    int64(8104928),
		"added_files_count":    41,
		"existing_files_count": 1163,
		"deleted_files_count":  0,
		"added_rows_count":     int64(120400),
		"existing_rows_count":  int64(859712),
		"deleted_rows_count":   int64(0),
	})

	files, err := ReadManifestList(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ReadManifestList: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("read %d manifests, want 1", len(files))
	}

	file := files[0]
	if file.Path != "s3://warehouse/db/orders/metadata/abc-m0.avro" {
		t.Errorf("Path = %q", file.Path)
	}
	if file.SequenceNumber != 12 {
		t.Errorf("SequenceNumber = %d, want 12", file.SequenceNumber)
	}
	if file.AddedFiles != 41 || file.ExistingFiles != 1163 || file.DeletedFiles != 0 {
		t.Errorf("file counts = %d/%d/%d, want 41/1163/0",
			file.AddedFiles, file.ExistingFiles, file.DeletedFiles)
	}
	if file.AddedRows != 120400 || file.ExistingRows != 859712 {
		t.Errorf("row counts = %d/%d, want 120400/859712", file.AddedRows, file.ExistingRows)
	}
}

func TestReadManifestListV1Spelling(t *testing.T) {
	// Version 1 spells the counts differently and has no content field. Both must read back
	// under the same names a caller uses for version 2.
	body := writeOCF(t, manifestListV1Schema, nil, map[string]any{
		"manifest_path":             "s3://warehouse/db/legacy/metadata/m0.avro",
		"manifest_length":           int64(4096),
		"partition_spec_id":         0,
		"added_snapshot_id":         int64(42),
		"added_data_files_count":    map[string]any{"int": 7},
		"existing_data_files_count": map[string]any{"int": 3},
		"deleted_data_files_count":  nil,
		"added_rows_count":          map[string]any{"long": int64(700)},
		"existing_rows_count":       nil,
		"deleted_rows_count":        nil,
	})

	files, err := ReadManifestList(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("ReadManifestList: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("read %d manifests, want 1", len(files))
	}

	file := files[0]
	if file.AddedFiles != 7 || file.ExistingFiles != 3 {
		t.Errorf("file counts = %d/%d, want 7/3", file.AddedFiles, file.ExistingFiles)
	}
	// A null count is no count, which for a count is zero.
	if file.DeletedFiles != 0 || file.ExistingRows != 0 {
		t.Errorf("absent counts = %d/%d, want 0/0", file.DeletedFiles, file.ExistingRows)
	}
	if file.AddedRows != 700 {
		t.Errorf("AddedRows = %d, want 700", file.AddedRows)
	}
	// Version 1 has no delete files, so an entry with no content field holds data.
	if file.Content != ContentData {
		t.Errorf("Content = %d, want %d", file.Content, ContentData)
	}
}

func TestReadManifestV2(t *testing.T) {
	spec := []byte(`[{"source-id":2,"field-id":1000,"name":"dt","transform":"day"}]`)
	body := writeOCF(t, manifestV2Schema,
		map[string][]byte{"partition-spec": spec, "format-version": []byte("2")},
		// An added entry leaves its sequence number out: it inherits the manifest's.
		map[string]any{
			"status":          StatusAdded,
			"snapshot_id":     map[string]any{"long": int64(8104928)},
			"sequence_number": nil,
			"data_file": map[string]any{
				"content":            ContentData,
				"file_path":          "s3://warehouse/db/orders/data/dt=2026-08-16/f.parquet",
				"file_format":        "PARQUET",
				"partition":          map[string]any{"dt": map[string]any{"int": 20681}},
				"record_count":       int64(12004),
				"file_size_in_bytes": int64(4_194_304),
			},
		},
		// An existing entry carries its own, from the snapshot that added it.
		map[string]any{
			"status":          StatusExisting,
			"snapshot_id":     map[string]any{"long": int64(7719301)},
			"sequence_number": map[string]any{"long": int64(9)},
			"data_file": map[string]any{
				"content":            ContentPositionDeletes,
				"file_path":          "s3://warehouse/db/orders/data/dt=2026-08-15/d.parquet",
				"file_format":        "PARQUET",
				"partition":          map[string]any{"dt": map[string]any{"int": 20680}},
				"record_count":       int64(412),
				"file_size_in_bytes": int64(18_432),
			},
		},
	)

	manifest, err := ReadManifest(bytes.NewReader(body), 12)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(manifest.Entries) != 2 {
		t.Fatalf("read %d entries, want 2", len(manifest.Entries))
	}
	if manifest.Truncated {
		t.Error("Truncated on a manifest of two entries")
	}

	added := manifest.Entries[0]
	if added.SequenceNumber == nil || *added.SequenceNumber != 12 {
		t.Errorf("an entry with no sequence number of its own did not inherit the manifest's: %v",
			added.SequenceNumber)
	}
	if added.DataFile.RecordCount != 12004 || added.DataFile.SizeBytes != 4_194_304 {
		t.Errorf("data file = %d records, %d bytes",
			added.DataFile.RecordCount, added.DataFile.SizeBytes)
	}

	existing := manifest.Entries[1]
	if existing.SequenceNumber == nil || *existing.SequenceNumber != 9 {
		t.Errorf("an entry's own sequence number was overwritten: %v", existing.SequenceNumber)
	}
	if existing.DataFile.Content != ContentPositionDeletes {
		t.Errorf("Content = %d, want %d", existing.DataFile.Content, ContentPositionDeletes)
	}

	// The spec out of the manifest's own metadata is what makes its partition values readable.
	if len(manifest.Spec) != 1 || manifest.Spec[0].Transform != "day" {
		t.Fatalf("Spec = %+v, want one day transform", manifest.Spec)
	}
	if got := FormatPartition(manifest.Spec, added.DataFile.Partition); got != "dt=2026-08-16" {
		t.Errorf("partition = %q, want dt=2026-08-16", got)
	}
}

func TestReadManifestV1(t *testing.T) {
	// A version 1 manifest has no sequence numbers at all, and its list reports zero for one.
	body := writeOCF(t, manifestV1Schema, nil, map[string]any{
		"status":      StatusAdded,
		"snapshot_id": map[string]any{"long": int64(42)},
		"data_file": map[string]any{
			"file_path":          "s3://warehouse/db/legacy/data/f.parquet",
			"file_format":        "PARQUET",
			"partition":          map[string]any{"id_bucket": map[string]any{"int": 7}},
			"record_count":       int64(100),
			"file_size_in_bytes": int64(2048),
		},
	})

	manifest, err := ReadManifest(bytes.NewReader(body), 0)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if len(manifest.Entries) != 1 {
		t.Fatalf("read %d entries, want 1", len(manifest.Entries))
	}

	entry := manifest.Entries[0]
	// No content field means data: version 1 has nothing else to hold.
	if entry.DataFile.Content != ContentData {
		t.Errorf("Content = %d, want %d", entry.DataFile.Content, ContentData)
	}
	if entry.SequenceNumber == nil || *entry.SequenceNumber != 0 {
		t.Errorf("SequenceNumber = %v, want 0", entry.SequenceNumber)
	}
	if entry.DataFile.Path != "s3://warehouse/db/legacy/data/f.parquet" {
		t.Errorf("Path = %q", entry.DataFile.Path)
	}
	// With no spec in the manifest's metadata the values are shown as they are stored.
	if got := FormatPartition(manifest.Spec, entry.DataFile.Partition); got != "id_bucket=7" {
		t.Errorf("partition = %q, want id_bucket=7", got)
	}
}

func TestReadManifestRejectsSomethingElse(t *testing.T) {
	if _, err := ReadManifest(bytes.NewReader([]byte("PAR1not-an-avro-file")), 0); err == nil {
		t.Error("ReadManifest accepted a file that is not an object container")
	}
	if _, err := ReadManifestList(bytes.NewReader(nil)); err == nil {
		t.Error("ReadManifestList accepted an empty file")
	}
}

func TestNames(t *testing.T) {
	if got := ContentName(ContentEqualityDeletes); got != "equality-deletes" {
		t.Errorf("ContentName = %q", got)
	}
	// A content value no version defines is data, which is what every file was before deletes
	// existed.
	if got := ContentName(99); got != "data" {
		t.Errorf("ContentName(99) = %q, want data", got)
	}
	if got := StatusName(StatusDeleted); got != "deleted" {
		t.Errorf("StatusName = %q", got)
	}
}
