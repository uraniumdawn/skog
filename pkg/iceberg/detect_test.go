// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"context"
	"errors"
	"testing"

	"github.com/uraniumdawn/skog/pkg/s3"
)

func TestIsTable(t *testing.T) {
	tests := []struct {
		name   string
		source *fakeSource
		prefix string
		want   bool
	}{
		{
			name: "a metadata level holding a document",
			source: newFakeSource().level("db/orders/metadata/", nil,
				"db/orders/metadata/00042-abc.metadata.json",
				"db/orders/metadata/snap-8104928.avro",
			),
			prefix: "db/orders/",
			want:   true,
		},
		{
			name: "a metadata level holding no document is not a table yet",
			source: newFakeSource().level("db/half/metadata/", nil,
				"db/half/metadata/snap-1.avro",
			),
			prefix: "db/half/",
			want:   false,
		},
		{
			name:   "a prefix with no metadata level",
			source: newFakeSource().level("db/plain/metadata/", nil),
			prefix: "db/plain/",
			want:   false,
		},
		{
			name: "a document past the first batch is still found",
			source: newFakeSource().
				level("db/busy/metadata/", nil, "db/busy/metadata/snap-1.avro").
				level("db/busy/metadata/", nil, "db/busy/metadata/v7.metadata.json"),
			prefix: "db/busy/",
			want:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := IsTable(context.Background(), test.source, "warehouse", test.prefix)
			if err != nil {
				t.Fatalf("IsTable: %v", err)
			}
			if got != test.want {
				t.Errorf("IsTable(%q) = %v, want %v", test.prefix, got, test.want)
			}
		})
	}
}

func TestIsTableStopsAtTheFirstDocument(t *testing.T) {
	// The answer is yes as soon as one document is seen, so a metadata level of a thousand
	// manifests is not walked to its end to say so.
	source := newFakeSource().
		level("db/orders/metadata/", nil, "db/orders/metadata/00001-abc.metadata.json").
		level("db/orders/metadata/", nil, "db/orders/metadata/snap-2.avro")

	if _, err := IsTable(context.Background(), source, "warehouse", "db/orders/"); err != nil {
		t.Fatalf("IsTable: %v", err)
	}
	if len(source.listed) != 1 {
		t.Errorf("listed %d batches, want 1", len(source.listed))
	}
}

func TestIsTableReportsFailure(t *testing.T) {
	source := newFakeSource()
	source.err = errors.New("access denied")

	if _, err := IsTable(context.Background(), source, "warehouse", "db/orders/"); err == nil {
		t.Error("IsTable swallowed a listing failure")
	}
}

func TestVersions(t *testing.T) {
	source := newFakeSource().level("db/orders/metadata/", nil,
		"db/orders/metadata/00040-aaa.metadata.json",
		"db/orders/metadata/00042-ccc.metadata.json",
		"db/orders/metadata/00041-bbb.metadata.json",
		"db/orders/metadata/snap-8104928.avro",
		"db/orders/metadata/abc-m0.avro",
	)

	versions, truncated, err := Versions(context.Background(), source, "warehouse", "db/orders/", 0)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if truncated {
		t.Error("truncated on a level that was walked whole")
	}

	// Only the metadata documents, newest first.
	want := []int{42, 41, 40}
	if len(versions) != len(want) {
		t.Fatalf("got %d versions, want %d", len(versions), len(want))
	}
	for i, number := range want {
		if versions[i].Number != number {
			t.Errorf("version %d = %d, want %d", i, versions[i].Number, number)
		}
	}
	if versions[0].File != "00042-ccc.metadata.json" {
		t.Errorf("File = %q", versions[0].File)
	}

	// With no version hint in the bucket, the highest number is a guess and is marked as one.
	if versions[0].Mark != MarkLatest {
		t.Errorf("Mark = %q, want %q", versions[0].Mark, MarkLatest)
	}
	if versions[1].Mark != "" {
		t.Errorf("a version behind the newest is marked %q", versions[1].Mark)
	}
}

func TestVersionsFollowsTheVersionHint(t *testing.T) {
	// A hadoop table keeps the pointer in the bucket, and it need not name the newest file: a
	// commit that failed after writing its document leaves a higher-numbered one behind.
	source := newFakeSource().
		level("db/hadoop/metadata/", nil,
			"db/hadoop/metadata/v7.metadata.json",
			"db/hadoop/metadata/v8.metadata.json",
		).
		object("db/hadoop/metadata/version-hint.text", []byte("7\n"))

	versions, _, err := Versions(context.Background(), source, "warehouse", "db/hadoop/", 0)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) != 2 {
		t.Fatalf("got %d versions, want 2", len(versions))
	}

	if versions[0].Number != 8 || versions[0].Mark != "" {
		t.Errorf("v8 = number %d, mark %q; want it unmarked", versions[0].Number, versions[0].Mark)
	}
	if versions[1].Number != 7 || versions[1].Mark != MarkHint {
		t.Errorf("v7 = number %d, mark %q; want %q", versions[1].Number, versions[1].Mark, MarkHint)
	}
}

func TestVersionsReportsTruncation(t *testing.T) {
	source := newFakeSource().
		level("db/orders/metadata/", nil, "db/orders/metadata/00001-a.metadata.json").
		level("db/orders/metadata/", nil, "db/orders/metadata/00002-b.metadata.json")

	_, truncated, err := Versions(context.Background(), source, "warehouse", "db/orders/", 1)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if !truncated {
		t.Error("a level cut short at the cap did not say so")
	}
}

func TestVersionNumber(t *testing.T) {
	tests := []struct {
		name string
		want int
	}{
		{"00042-8f3c-4a1b.metadata.json", 42},
		{"v7.metadata.json", 7},
		{"00003-abc.gz.metadata.json", 3},
		{"metadata.json", -1},
		{"vNext.metadata.json", -1},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := versionNumber(test.name); got != test.want {
				t.Errorf("versionNumber(%q) = %d, want %d", test.name, got, test.want)
			}
		})
	}
}

func TestEachPageWalksTheWholeLevel(t *testing.T) {
	source := newFakeSource().
		level("db/", []string{"db/a/"}, "db/one.json").
		level("db/", []string{"db/b/"}, "db/two.json")

	var seen []string
	truncated, err := eachPage(
		context.Background(), source, "warehouse", "db/", 0,
		func(listing *s3.Listing) bool {
			for _, object := range listing.Objects {
				seen = append(seen, object.Key)
			}
			return true
		},
	)
	if err != nil {
		t.Fatalf("eachPage: %v", err)
	}
	if truncated {
		t.Error("truncated on a level that ended on its own")
	}
	if len(seen) != 2 {
		t.Errorf("saw %v, want both batches", seen)
	}
}
