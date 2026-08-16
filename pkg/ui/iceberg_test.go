// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"strings"
	"testing"

	"github.com/uraniumdawn/skog/pkg/config"
	"github.com/uraniumdawn/skog/pkg/iceberg"
)

// tableMetadata is a format version 2 document with the parts the pages read: a schema, a
// partition spec over two of its columns, a sort order, refs and properties.
const tableMetadata = `{
  "format-version": 2,
  "table-uuid": "9d0c1e2a-0000-4000-8000-000000000001",
  "location": "s3://warehouse/analytics/orders",
  "last-updated-ms": 1755342120000,
  "current-snapshot-id": 8104928,
  "current-schema-id": 0,
  "schemas": [{"schema-id": 0, "fields": [
    {"id": 1, "name": "order_id", "required": true, "type": "long"},
    {"id": 2, "name": "ts", "required": true, "type": "timestamptz"},
    {"id": 4, "name": "region", "required": false, "type": "string"}
  ]}],
  "default-spec-id": 0,
  "partition-specs": [{"spec-id": 0, "fields": [
    {"source-id": 2, "field-id": 1000, "name": "ts_day", "transform": "day"},
    {"source-id": 4, "field-id": 1001, "name": "region", "transform": "identity"}
  ]}],
  "default-sort-order-id": 1,
  "sort-orders": [
    {"order-id": 0, "fields": []},
    {"order-id": 1, "fields": [
      {"source-id": 2, "transform": "identity", "direction": "desc", "null-order": "nulls-last"},
      {"source-id": 9, "transform": "bucket[8]", "direction": "asc", "null-order": ""}
    ]}
  ],
  "properties": {"write.format.default": "parquet"},
  "refs": {"main": {"snapshot-id": 8104928, "type": "branch"}},
  "snapshots": [{
    "snapshot-id": 8104928,
    "timestamp-ms": 1755342120000,
    "manifest-list": "s3://warehouse/analytics/orders/metadata/snap-8104928.avro",
    "summary": {"operation": "append", "added-records": "120400", "total-records": "980112"}
  }]
}`

// testColors is the least a Menu needs: it reads only the keybinding colours.
func testColors() *config.ColorConfig {
	return &config.ColorConfig{}
}

func parseTableMetadata(t *testing.T) *iceberg.Metadata {
	t.Helper()

	metadata, err := iceberg.ParseMetadata(strings.NewReader(tableMetadata))
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}
	return metadata
}

func TestSpecText(t *testing.T) {
	metadata := parseTableMetadata(t)

	got := specText(metadata.DefaultPartitionSpec(), metadata.CurrentSchema())
	want := "ts_day = day(ts), region = identity(region)"
	if got != want {
		t.Errorf("specText() = %q, want %q", got, want)
	}

	if got := specText(nil, metadata.CurrentSchema()); got != "" {
		t.Errorf("specText(nil) = %q, want empty", got)
	}
	// An unpartitioned table has a spec with no fields, which is not something to show.
	if got := specText(&iceberg.PartitionSpec{SpecID: 0}, metadata.CurrentSchema()); got != "" {
		t.Errorf("specText(empty spec) = %q, want empty", got)
	}
}

func TestSortOrderText(t *testing.T) {
	metadata := parseTableMetadata(t)

	// The identity transform is not written out — it names the column and nothing else — and a
	// term over a column the schema no longer has falls back to its field id.
	got := sortOrderText(metadata.DefaultSortOrder(), metadata.CurrentSchema())
	want := "ts desc nulls-last, bucket[8](field-9) asc"
	if got != want {
		t.Errorf("sortOrderText() = %q, want %q", got, want)
	}

	if got := sortOrderText(nil, metadata.CurrentSchema()); got != "" {
		t.Errorf("sortOrderText(nil) = %q, want empty", got)
	}
}

func TestOverviewText(t *testing.T) {
	metadata := parseTableMetadata(t)
	target := IcebergVersionTarget{
		Bucket: "warehouse",
		Prefix: "analytics/orders/",
		Key:    "analytics/orders/metadata/00042-abc.metadata.json",
	}

	text := overviewText(target, metadata)

	for _, want := range []string{
		"Table:", "orders",
		"Metadata:", "00042-abc.metadata.json",
		"Format version:", "2",
		"Location:", "s3://warehouse/analytics/orders",
		"Current snapshot:", "8104928",
		"Snapshots:", "1",
		"Schema:", "3 columns (schema-id 0)",
		"Partition spec:", "ts_day = day(ts)",
		"Sort order:", "ts desc nulls-last",
		"Refs:", "main:", "branch 8104928",
		"Properties:", "write.format.default",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("the overview does not mention %q:\n%s", want, text)
		}
	}
}

func TestOverviewTextOfATableWithNoSnapshot(t *testing.T) {
	// A table created and never written to has no current snapshot, and says so rather than
	// leaving the line off, which would read as a document that forgot to mention it.
	const empty = `{"format-version": 2, "location": "s3://w/t", "schemas": [], "snapshots": []}`

	metadata, err := iceberg.ParseMetadata(strings.NewReader(empty))
	if err != nil {
		t.Fatalf("parsing the fixture: %v", err)
	}

	text := overviewText(IcebergVersionTarget{Prefix: "t/", Key: "t/metadata/v1.metadata.json"}, metadata)
	if !strings.Contains(text, "Current snapshot:") || !strings.Contains(text, "none") {
		t.Errorf("a table with no snapshot does not say so:\n%s", text)
	}
}

func TestVersionsTitle(t *testing.T) {
	tests := []struct {
		name  string
		found icebergVersions
		want  string
	}{
		{
			name: "with no hint the newest is a guess, and the title says so",
			found: icebergVersions{versions: []iceberg.Version{
				{Number: 2, Mark: iceberg.MarkLatest}, {Number: 1},
			}},
			want: " /w/t/ metadata [2] (no catalog, newest by name) ",
		},
		{
			name: "a hinted version is named for what it is",
			found: icebergVersions{versions: []iceberg.Version{
				{Number: 2}, {Number: 1, Mark: iceberg.MarkHint},
			}},
			want: " /w/t/ metadata [2] (version-hint.text) ",
		},
		{
			name: "a level cut short at the cap is marked, since a newer one may not be listed",
			found: icebergVersions{
				versions:  []iceberg.Version{{Number: 1, Mark: iceberg.MarkLatest}},
				truncated: true,
			},
			want: " /w/t/ metadata [>1] (no catalog, newest by name) ",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := versionsTitle("/w/t/", test.found); got != test.want {
				t.Errorf("versionsTitle() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestSummaryCount(t *testing.T) {
	snapshot := iceberg.Snapshot{Summary: map[string]string{
		"added-records": "120400",
		"odd":           "not a number",
	}}

	if got := summaryCount(snapshot, "added-records"); got != "120,400" {
		t.Errorf("summaryCount = %q, want a grouped 120,400", got)
	}
	// A figure the writer did not record is blank, not zero: a zero would be a claim the
	// snapshot never made.
	if got := summaryCount(snapshot, "deleted-records"); got != "" {
		t.Errorf("summaryCount of an absent key = %q, want empty", got)
	}
	if got := summaryCount(snapshot, "odd"); got != "not a number" {
		t.Errorf("summaryCount of a non-number = %q, want it shown as written", got)
	}
}

func TestTableName(t *testing.T) {
	tests := []struct {
		prefix string
		want   string
	}{
		{"analytics/orders/", "orders"},
		{"orders/", "orders"},
		{"", "/"},
	}

	for _, test := range tests {
		t.Run(test.prefix, func(t *testing.T) {
			if got := tableName(test.prefix); got != test.want {
				t.Errorf("tableName(%q) = %q, want %q", test.prefix, got, test.want)
			}
		})
	}
}

func TestFilterIcebergRows(t *testing.T) {
	rows := [][]string{
		{"00001-a.parquet", "data", "added"},
		{"00002-b.parquet", "position-deletes", "existing"},
		{"00003-c.parquet", "data", "deleted"},
	}

	t.Run("no filter keeps every row and its place", func(t *testing.T) {
		filtered, index := filterIcebergRows(rows, "")
		if len(filtered) != 3 {
			t.Fatalf("kept %d rows, want 3", len(filtered))
		}
		for i, at := range index {
			if at != i {
				t.Errorf("index[%d] = %d, want %d", i, at, i)
			}
		}
	})

	t.Run("a match on any cell keeps the row", func(t *testing.T) {
		filtered, index := filterIcebergRows(rows, "position-deletes")
		if len(filtered) != 1 || len(index) != 1 {
			t.Fatalf("kept %d rows, want 1", len(filtered))
		}
		// The index is what the keypress uses to find the row it acts on, so it has to point at
		// the row's place before filtering.
		if index[0] != 1 {
			t.Errorf("index = %d, want 1", index[0])
		}
		if filtered[0][0] != "00002-b.parquet" {
			t.Errorf("kept %q", filtered[0][0])
		}
	})

	t.Run("a filter matching nothing keeps nothing", func(t *testing.T) {
		filtered, index := filterIcebergRows(rows, "zzzz")
		if len(filtered) != 0 || len(index) != 0 {
			t.Errorf("kept %d rows, want none", len(filtered))
		}
	})
}

func TestIcebergTitle(t *testing.T) {
	if got := icebergTitle("files", 3, 3); got != " files [3] " {
		t.Errorf("a page showing everything = %q", got)
	}
	// A page filling in batches says how much of it is loaded, the way the viewer does.
	if got := icebergTitle("files", 500, 1204); got != " files [500/1204] " {
		t.Errorf("a partly loaded page = %q", got)
	}
}

func TestIcebergMenusFollowWhatIsLeft(t *testing.T) {
	if got := icebergLevelMenu(""); got != IcebergLevelPageMenu {
		t.Errorf("a complete level offers %q", got)
	}
	if got := icebergLevelMenu("token"); got != IcebergLevelBatchedPageMenu {
		t.Errorf("a level with a batch left offers %q", got)
	}
	if got := icebergFilesMenu(10, 10); got != IcebergFilesPageMenu {
		t.Errorf("a fully shown files page offers %q", got)
	}
	if got := icebergFilesMenu(500, 1204); got != IcebergFilesBatchedPageMenu {
		t.Errorf("a partly shown files page offers %q", got)
	}
	if got := icebergSnapshotsMenu(3, 3); got != IcebergSnapshotsPageMenu {
		t.Errorf("a fully shown snapshots page offers %q", got)
	}
	if got := icebergSnapshotsMenu(1, 3); got != IcebergSnapshotsBatchedPageMenu {
		t.Errorf("a partly shown snapshots page offers %q", got)
	}
}

func TestOriginSuffix(t *testing.T) {
	// A file opened from the S3 hierarchy keeps the page key it has always had.
	if got := originSuffix(""); got != "" {
		t.Errorf("originSuffix() = %q, want empty", got)
	}
	// One opened from an Iceberg manifest is a page of its own, so that <h> out of it goes back
	// to the manifest rather than to the object's metadata.
	if got := originSuffix("p:iceberg:/w/t/#1!m:files"); got != ":from:p:iceberg:/w/t/#1!m:files" {
		t.Errorf("originSuffix() = %q", got)
	}
}

func TestIcebergMenusAreRegistered(t *testing.T) {
	menu := NewMenu(testColors())

	for _, name := range []string{
		IcebergBucketsPageMenu,
		IcebergLevelPageMenu,
		IcebergLevelBatchedPageMenu,
		IcebergTablePageMenu,
		IcebergOverviewPageMenu,
		IcebergSchemaPageMenu,
		IcebergSnapshotsPageMenu,
		IcebergSnapshotsBatchedPageMenu,
		IcebergManifestsPageMenu,
		IcebergFilesPageMenu,
		IcebergFilesBatchedPageMenu,
	} {
		bindings, ok := (*menu.Map)[name]
		if !ok {
			t.Errorf("%s has no keybindings, so its pages would show an empty bar", name)
			continue
		}
		for _, binding := range *bindings {
			if _, ok := keys[binding]; !ok {
				t.Errorf("%s names %q, which is not a key", name, binding)
			}
		}
	}
}

// TestIcebergPagesNeverDelete pins the one rule every Iceberg page follows: none of them offers
// <x>. A data or metadata file deleted out from under a table breaks it in a way Iceberg cannot
// repair, and in yolo mode nothing would be asked first.
func TestIcebergPagesNeverDelete(t *testing.T) {
	menu := NewMenu(testColors())

	for name, bindings := range *menu.Map {
		if !strings.HasPrefix(name, "Iceberg") {
			continue
		}
		for _, binding := range *bindings {
			if binding == "delete" {
				t.Errorf("%s offers <x>", name)
			}
		}
	}
}
