// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/hamba/avro/v2/ocf"
)

// Statuses of a manifest entry.
const (
	StatusExisting = 0
	StatusAdded    = 1
	StatusDeleted  = 2
)

// Contents of a manifest or of a file it lists.
const (
	ContentData            = 0
	ContentPositionDeletes = 1
	ContentEqualityDeletes = 2
)

// maxManifestEntries caps what one manifest is read into memory as. A writer bounds a manifest
// at a few megabytes, so this is far above any file written on purpose; it is here so that a
// malformed one cannot take the application down with it.
const maxManifestEntries = 500_000

// ManifestFile is one entry of a snapshot's manifest list: a manifest, and what it holds.
//
// The counts are spelled added_files_count in format version 2 and added_data_files_count in
// version 1. Both are read, so a caller sees the same field whichever wrote the file.
type ManifestFile struct {
	Path              string
	Length            int64
	SpecID            int
	Content           int
	SequenceNumber    int64
	MinSequenceNumber int64
	AddedSnapshotID   int64
	AddedFiles        int64
	ExistingFiles     int64
	DeletedFiles      int64
	AddedRows         int64
	ExistingRows      int64
	DeletedRows       int64
}

// Manifest is one manifest file: the data or delete files it lists, and the partition spec it
// was written under.
type Manifest struct {
	Entries []ManifestEntry
	// Spec is the partitioning the manifest records in its own avro metadata, which is what
	// makes its partition values readable. It is empty for an unpartitioned table, and for a
	// manifest that carries no spec.
	Spec []PartitionField
	// Truncated reports that the manifest holds more entries than were read.
	Truncated bool
}

// ManifestEntry is one file of a manifest, and what the snapshot did with it.
type ManifestEntry struct {
	Status     int
	SnapshotID *int64
	// SequenceNumber is the entry's own, or the manifest's where the entry leaves it out: an
	// added file inherits the sequence number of the manifest it was added in.
	SequenceNumber *int64
	DataFile       DataFile
}

// DataFile is the file a manifest entry points at.
type DataFile struct {
	// Content says whether the file holds rows or deletes. Version 1 has no such field, and
	// version 1 has no delete files, so its absence means data.
	Content     int
	Path        string
	Format      string
	RecordCount int64
	SizeBytes   int64
	Partition   map[string]any
}

// ContentName renders a content value the way the pages label it.
func ContentName(content int) string {
	switch content {
	case ContentPositionDeletes:
		return "position-deletes"
	case ContentEqualityDeletes:
		return "equality-deletes"
	default:
		return "data"
	}
}

// StatusName renders an entry's status the way the pages label it.
func StatusName(status int) string {
	switch status {
	case StatusAdded:
		return "added"
	case StatusDeleted:
		return "deleted"
	default:
		return "existing"
	}
}

// ReadManifestList reads a snapshot's manifest list.
func ReadManifestList(r io.Reader) ([]ManifestFile, error) {
	decoder, err := ocf.NewDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("reading manifest list: %w", err)
	}

	var files []ManifestFile
	for decoder.HasNext() {
		var record any
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("reading manifest list: %w", err)
		}

		fields, ok := record.(map[string]any)
		if !ok {
			continue
		}
		files = append(files, manifestFile(fields))
	}

	if err := decoder.Error(); err != nil {
		return nil, fmt.Errorf("reading manifest list: %w", err)
	}
	return files, nil
}

// manifestFile reads one manifest list record, under either version's field names.
func manifestFile(fields map[string]any) ManifestFile {
	return ManifestFile{
		Path:              text(fields, "manifest_path"),
		Length:            number(fields, "manifest_length"),
		SpecID:            int(number(fields, "partition_spec_id")),
		Content:           int(number(fields, "content")),
		SequenceNumber:    number(fields, "sequence_number"),
		MinSequenceNumber: number(fields, "min_sequence_number"),
		AddedSnapshotID:   number(fields, "added_snapshot_id"),
		AddedFiles:        number(fields, "added_files_count", "added_data_files_count"),
		ExistingFiles:     number(fields, "existing_files_count", "existing_data_files_count"),
		DeletedFiles:      number(fields, "deleted_files_count", "deleted_data_files_count"),
		AddedRows:         number(fields, "added_rows_count"),
		ExistingRows:      number(fields, "existing_rows_count"),
		DeletedRows:       number(fields, "deleted_rows_count"),
	}
}

// ReadManifest reads one manifest. inherited is the sequence number of the manifest as its
// manifest list records it, which is what an entry that leaves its own out is given.
func ReadManifest(r io.Reader, inherited int64) (*Manifest, error) {
	decoder, err := ocf.NewDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}

	manifest := &Manifest{Spec: manifestSpec(decoder.Metadata())}

	for decoder.HasNext() {
		if len(manifest.Entries) >= maxManifestEntries {
			manifest.Truncated = true
			break
		}

		var record any
		if err := decoder.Decode(&record); err != nil {
			return nil, fmt.Errorf("reading manifest: %w", err)
		}

		fields, ok := record.(map[string]any)
		if !ok {
			continue
		}
		manifest.Entries = append(manifest.Entries, manifestEntry(fields, inherited))
	}

	if err := decoder.Error(); err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	return manifest, nil
}

// manifestEntry reads one manifest entry, applying the inheritance rule for its sequence number.
func manifestEntry(fields map[string]any, inherited int64) ManifestEntry {
	entry := ManifestEntry{
		Status:     int(number(fields, "status")),
		SnapshotID: optional(fields, "snapshot_id"),
		// An entry that leaves its sequence number out takes the manifest's. Format version 1
		// has no sequence numbers at all, and its manifest list reports zero for one, which is
		// what a version 1 entry then carries.
		SequenceNumber: optional(fields, "sequence_number"),
	}
	if entry.SequenceNumber == nil {
		seq := inherited
		entry.SequenceNumber = &seq
	}

	if file, ok := fields["data_file"].(map[string]any); ok {
		entry.DataFile = DataFile{
			Content:     int(number(file, "content")),
			Path:        text(file, "file_path"),
			Format:      text(file, "file_format"),
			RecordCount: number(file, "record_count"),
			SizeBytes:   number(file, "file_size_in_bytes"),
		}
		if partition, ok := file["partition"].(map[string]any); ok {
			entry.DataFile.Partition = partition
		}
	}

	return entry
}

// manifestSpec reads the partition spec a manifest records in its avro key-value metadata.
//
// Iceberg writes the spec into every manifest it produces, so the fields a partition value
// belongs to are read from the same file as the values, and no table metadata has to be fetched
// to render them. A manifest without it is read all the same, with its values shown as stored.
func manifestSpec(metadata map[string][]byte) []PartitionField {
	raw, ok := metadata["partition-spec"]
	if !ok {
		return nil
	}

	var fields []PartitionField
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil
	}
	return fields
}

// text is the string held by the first of names the record carries.
func text(fields map[string]any, names ...string) string {
	for _, name := range names {
		if value, ok := fields[name]; ok {
			if s, ok := value.(string); ok {
				return s
			}
		}
	}
	return ""
}

// number is the integer held by the first of names the record carries a value for. A field that
// is absent, or present and null, is zero — which for every count here is what it means.
func number(fields map[string]any, names ...string) int64 {
	for _, name := range names {
		value, ok := fields[name]
		if !ok || value == nil {
			continue
		}
		if n, ok := asInt64(value); ok {
			return n
		}
	}
	return 0
}

// optional is the integer held by name, nil where the record leaves it out or writes null. It
// is what tells "absent" from "zero", which for a sequence number is the whole question.
func optional(fields map[string]any, name string) *int64 {
	value, ok := fields[name]
	if !ok || value == nil {
		return nil
	}
	n, ok := asInt64(value)
	if !ok {
		return nil
	}
	return &n
}
