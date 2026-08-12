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

// levelPage is one canned ListObjectsV2 response of a delimited listing.
type levelPage struct {
	prefixes []string
	objects  []types.Object
	next     string // the continuation token S3 hands out with this page
}

// levelLister serves canned pages and records what each request asked for.
type levelLister struct {
	pages   []levelPage
	calls   int
	tokens  []string
	maxKeys []int32
}

func (l *levelLister) ListObjectsV2(
	_ context.Context,
	in *awss3.ListObjectsV2Input,
	_ ...func(*awss3.Options),
) (*awss3.ListObjectsV2Output, error) {
	l.tokens = append(l.tokens, aws.ToString(in.ContinuationToken))
	l.maxKeys = append(l.maxKeys, aws.ToInt32(in.MaxKeys))
	page := l.pages[l.calls]
	l.calls++

	out := &awss3.ListObjectsV2Output{Contents: page.objects}
	for _, prefix := range page.prefixes {
		out.CommonPrefixes = append(out.CommonPrefixes, types.CommonPrefix{
			Prefix: aws.String(prefix),
		})
	}
	if page.next != "" {
		out.IsTruncated = aws.Bool(true)
		out.NextContinuationToken = aws.String(page.next)
	}
	return out, nil
}

func TestListObjectsBatches(t *testing.T) {
	modified := time.Date(2026, 8, 10, 11, 2, 14, 0, time.UTC)
	key := func(name string) types.Object {
		return object(name, 10, modified, types.ObjectStorageClassStandard)
	}

	t.Run("a batch is one request, asking for the configured entries", func(t *testing.T) {
		lister := &levelLister{pages: []levelPage{
			{prefixes: []string{"data/"}, objects: []types.Object{key("12.json")}, next: "t1"},
			{objects: []types.Object{key("13.json")}},
		}}

		listing, err := listObjects(context.Background(), lister, listRequest{
			bucket: "bucket", maxEntries: 500,
		})
		if err != nil {
			t.Fatalf("listObjects() error = %v", err)
		}

		if lister.calls != 1 {
			t.Errorf("ListObjectsV2 calls = %d, want 1", lister.calls)
		}
		if lister.maxKeys[0] != 500 {
			t.Errorf("request asked for %d entries, want 500", lister.maxKeys[0])
		}
		if listing.Total() != 2 {
			t.Errorf("Total() = %d, want the 2 entries of that request", listing.Total())
		}
		if listing.NextToken != "t1" {
			t.Errorf("NextToken = %q, want %q", listing.NextToken, "t1")
		}
	})

	t.Run("a level that ends has nothing to continue", func(t *testing.T) {
		lister := &levelLister{pages: []levelPage{
			{objects: []types.Object{key("12.json")}},
		}}

		listing, err := listObjects(context.Background(), lister, listRequest{
			bucket: "bucket", maxEntries: 1000,
		})
		if err != nil {
			t.Fatalf("listObjects() error = %v", err)
		}

		if listing.NextToken != "" {
			t.Errorf("NextToken = %q, want it empty", listing.NextToken)
		}
	})

	t.Run("the next batch resumes from the token it is given", func(t *testing.T) {
		lister := &levelLister{pages: []levelPage{
			{prefixes: []string{"time/"}, objects: []types.Object{key("15.json")}},
		}}

		listing, err := listObjects(context.Background(), lister, listRequest{
			bucket: "bucket", prefix: "data/", token: "t1", maxEntries: 1000,
		})
		if err != nil {
			t.Fatalf("listObjects() error = %v", err)
		}

		if lister.tokens[0] != "t1" {
			t.Errorf("request used token %q, want %q", lister.tokens[0], "t1")
		}
		if len(listing.Prefixes) != 1 || listing.Prefixes[0] != "time/" {
			t.Errorf("Prefixes = %v, want [time/]", listing.Prefixes)
		}
		if listing.Prefix != "data/" {
			t.Errorf("Prefix = %q, want %q", listing.Prefix, "data/")
		}
	})

	t.Run("the folder marker of the level is not an entry in it", func(t *testing.T) {
		lister := &levelLister{pages: []levelPage{
			{objects: []types.Object{key("data/"), key("data/12.json")}},
		}}

		listing, err := listObjects(context.Background(), lister, listRequest{
			bucket: "bucket", prefix: "data/", maxEntries: 1000,
		})
		if err != nil {
			t.Fatalf("listObjects() error = %v", err)
		}

		if len(listing.Objects) != 1 || listing.Objects[0].Key != "data/12.json" {
			t.Errorf("Objects = %v, want just data/12.json", listing.Objects)
		}
	})
}
