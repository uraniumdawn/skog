// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import (
	"context"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// pagedLister serves canned ListObjectsV2 pages, so the walk can be tested without an S3.
type pagedLister struct {
	pages [][]types.Object
	calls int
}

func (l *pagedLister) ListObjectsV2(
	_ context.Context,
	_ *awss3.ListObjectsV2Input,
	_ ...func(*awss3.Options),
) (*awss3.ListObjectsV2Output, error) {
	page := l.pages[l.calls]
	l.calls++
	more := l.calls < len(l.pages)

	out := &awss3.ListObjectsV2Output{
		Contents:    page,
		IsTruncated: aws.Bool(more),
	}
	if more {
		out.NextContinuationToken = aws.String("next")
	}
	return out, nil
}

func object(key string, size int64, modified time.Time, class types.ObjectStorageClass) types.Object {
	return types.Object{
		Key:          aws.String(key),
		Size:         aws.Int64(size),
		LastModified: aws.Time(modified),
		StorageClass: class,
	}
}

func TestStatPrefix(t *testing.T) {
	older := time.Date(2026, 7, 2, 9, 41, 55, 0, time.UTC)
	newer := time.Date(2026, 8, 10, 11, 2, 14, 0, time.UTC)

	tests := []struct {
		name       string
		prefix     string
		maxScanned int
		pages      [][]types.Object
		want       PrefixStats
		wantClass  string
		wantCalls  int
	}{
		{
			name:       "aggregates every page",
			maxScanned: 100,
			pages: [][]types.Object{
				{
					object("data/12.json", 100, older, types.ObjectStorageClassStandard),
					object("data/13.json", 200, newer, types.ObjectStorageClassStandard),
				},
				{object("data/time/02/14.json", 300, older, types.ObjectStorageClassStandard)},
			},
			want: PrefixStats{
				Objects:        3,
				Size:           600,
				LastModified:   newer,
				StorageClasses: []string{"STANDARD"},
			},
			wantClass: "STANDARD",
			wantCalls: 2,
		},
		{
			name:       "distinct storage classes read as mixed",
			maxScanned: 100,
			pages: [][]types.Object{{
				object("a", 1, older, types.ObjectStorageClassStandard),
				object("b", 1, older, types.ObjectStorageClassGlacier),
			}},
			want: PrefixStats{
				Objects:        2,
				Size:           2,
				LastModified:   older,
				StorageClasses: []string{"GLACIER", "STANDARD"},
			},
			wantClass: mixedStorageClass,
			wantCalls: 1,
		},
		{
			name:       "the folder marker of the prefix is not content",
			prefix:     "data/",
			maxScanned: 100,
			pages: [][]types.Object{{
				object("data/", 0, older, types.ObjectStorageClassStandard),
				object("data/12.json", 100, newer, types.ObjectStorageClassStandard),
			}},
			want: PrefixStats{
				Objects:        1,
				Size:           100,
				LastModified:   newer,
				StorageClasses: []string{"STANDARD"},
			},
			wantClass: "STANDARD",
			wantCalls: 1,
		},
		{
			name:       "the cap stops the walk and marks the aggregate partial",
			maxScanned: 2,
			pages: [][]types.Object{
				{
					object("a", 1, older, types.ObjectStorageClassStandard),
					object("b", 1, older, types.ObjectStorageClassStandard),
				},
				{object("c", 1, newer, types.ObjectStorageClassStandard)},
			},
			want: PrefixStats{
				Objects:        2,
				Size:           2,
				LastModified:   older,
				StorageClasses: []string{"STANDARD"},
				Partial:        true,
			},
			wantClass: "STANDARD",
			wantCalls: 1,
		},
		{
			name:       "hitting the cap on the last page is a complete aggregate",
			maxScanned: 2,
			pages: [][]types.Object{{
				object("a", 1, older, types.ObjectStorageClassStandard),
				object("b", 1, older, types.ObjectStorageClassStandard),
			}},
			want: PrefixStats{
				Objects:        2,
				Size:           2,
				LastModified:   older,
				StorageClasses: []string{"STANDARD"},
			},
			wantClass: "STANDARD",
			wantCalls: 1,
		},
		{
			name:       "a zero cap walks everything",
			maxScanned: 0,
			pages: [][]types.Object{
				{object("a", 1, older, types.ObjectStorageClassStandard)},
				{object("b", 1, older, types.ObjectStorageClassStandard)},
				{object("c", 1, newer, types.ObjectStorageClassStandard)},
			},
			want: PrefixStats{
				Objects:        3,
				Size:           3,
				LastModified:   newer,
				StorageClasses: []string{"STANDARD"},
			},
			wantClass: "STANDARD",
			wantCalls: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lister := &pagedLister{pages: tt.pages}
			var progress []int

			got, err := statPrefix(context.Background(), lister, statRequest{
				bucket:     "bucket",
				prefix:     tt.prefix,
				maxScanned: tt.maxScanned,
				onPage:     func(scanned int) { progress = append(progress, scanned) },
			})
			if err != nil {
				t.Fatalf("statPrefix() error = %v", err)
			}

			if got.Objects != tt.want.Objects {
				t.Errorf("Objects = %d, want %d", got.Objects, tt.want.Objects)
			}
			if got.Size != tt.want.Size {
				t.Errorf("Size = %d, want %d", got.Size, tt.want.Size)
			}
			if !got.LastModified.Equal(tt.want.LastModified) {
				t.Errorf("LastModified = %v, want %v", got.LastModified, tt.want.LastModified)
			}
			if got.Partial != tt.want.Partial {
				t.Errorf("Partial = %v, want %v", got.Partial, tt.want.Partial)
			}
			if !equalStrings(got.StorageClasses, tt.want.StorageClasses) {
				t.Errorf("StorageClasses = %v, want %v", got.StorageClasses, tt.want.StorageClasses)
			}
			if class := got.StorageClass(); class != tt.wantClass {
				t.Errorf("StorageClass() = %q, want %q", class, tt.wantClass)
			}
			if lister.calls != tt.wantCalls {
				t.Errorf("ListObjectsV2 calls = %d, want %d", lister.calls, tt.wantCalls)
			}
			if len(progress) != tt.wantCalls {
				t.Errorf("progress reports = %d, want one per page (%d)", len(progress), tt.wantCalls)
			}
		})
	}
}

// An empty prefix has no storage class to report until it is walked.
func TestPrefixStatsStorageClassOfEmptyAggregate(t *testing.T) {
	stats := &PrefixStats{}
	if got := stats.StorageClass(); got != "-" {
		t.Errorf("StorageClass() = %q, want %q", got, "-")
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
