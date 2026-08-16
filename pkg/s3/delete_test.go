// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// deleter serves canned listings and accepts the deletes made against them, so a recursive
// delete can be tested without an S3.
type deleter struct {
	pagedLister
	// refused is the key the service answers for with an error, standing for one a policy keeps.
	refused string
	deleted []string
	// batches is how many keys each request carried, in order.
	batches []int
}

func (d *deleter) DeleteObjects(
	_ context.Context,
	in *awss3.DeleteObjectsInput,
	_ ...func(*awss3.Options),
) (*awss3.DeleteObjectsOutput, error) {
	d.batches = append(d.batches, len(in.Delete.Objects))

	out := &awss3.DeleteObjectsOutput{}
	for _, o := range in.Delete.Objects {
		if aws.ToString(o.Key) == d.refused {
			out.Errors = append(out.Errors, types.Error{
				Key:     o.Key,
				Message: aws.String("AccessDenied"),
			})
			continue
		}
		d.deleted = append(d.deleted, aws.ToString(o.Key))
	}
	return out, nil
}

func TestDeletePrefixRemovesTheWholeSubtree(t *testing.T) {
	api := &deleter{pagedLister: pagedLister{pages: [][]types.Object{
		{key("data/"), key("data/12.json")},
		{key("data/time/13.json")},
	}}}

	var progress []int
	result, err := deletePrefix(context.Background(), api, deleteRequest{
		bucket: "bucket", prefix: "data/", maxEntries: 1000,
		onDeleted: func(done int) { progress = append(progress, done) },
	})
	if err != nil {
		t.Fatalf("deletePrefix() error = %v", err)
	}

	// The folder marker is the level itself, and the level is what is being removed: unlike a
	// download, which skips it, a delete has to take it too or the folder outlives its content.
	want := []string{"data/", "data/12.json", "data/time/13.json"}
	if strings.Join(api.deleted, ",") != strings.Join(want, ",") {
		t.Errorf("deleted = %v, want %v", api.deleted, want)
	}
	if result.Objects != 3 {
		t.Errorf("result.Objects = %d, want 3", result.Objects)
	}
	// Both pages fit one batch, so they leave together in one request.
	if len(api.batches) != 1 || api.batches[0] != 3 {
		t.Errorf("batches = %v, want one request of 3 keys", api.batches)
	}
	if len(progress) != 1 || progress[0] != 3 {
		t.Errorf("progress = %v, want one report of 3", progress)
	}
}

func TestDeletePrefixRefusesWhatIsNotALevel(t *testing.T) {
	// A prefix that does not end with the delimiter is a string keys begin with: "data/2024"
	// covers "data/2024-backup/" as much as "data/2024/".
	for _, prefix := range []string{"", "data", "data/2024"} {
		t.Run("prefix "+prefix, func(t *testing.T) {
			api := &deleter{}

			result, err := deletePrefix(context.Background(), api, deleteRequest{
				bucket: "bucket", prefix: prefix, maxEntries: 1000,
			})
			if err == nil {
				t.Fatalf("deletePrefix(%q) error = nil, want a refusal", prefix)
			}
			if result.Objects != 0 || len(api.batches) != 0 {
				t.Errorf("%d objects deleted in %d requests, want nothing touched",
					result.Objects, len(api.batches))
			}
			if api.calls != 0 {
				t.Errorf("the bucket was listed %d times, want not at all", api.calls)
			}
		})
	}
}

func TestDeletePrefixCountsWhatWentWhenAKeyIsRefused(t *testing.T) {
	api := &deleter{
		pagedLister: pagedLister{pages: [][]types.Object{
			{key("data/12.json"), key("data/13.json"), key("data/14.json")},
		}},
		refused: "data/13.json",
	}

	result, err := deletePrefix(context.Background(), api, deleteRequest{
		bucket: "bucket", prefix: "data/", maxEntries: 1000,
	})
	if err == nil {
		t.Fatal("deletePrefix() error = nil, want the refused key reported")
	}
	if !strings.Contains(err.Error(), "data/13.json") {
		t.Errorf("error = %q, want the refused key named", err)
	}
	// The rest of the batch went, and there is no undo: what the delete did has to be counted
	// even though it failed.
	if result.Objects != 2 {
		t.Errorf("result.Objects = %d, want the 2 keys that went", result.Objects)
	}
}

func TestDeletePrefixBatchesAtTheRequestCap(t *testing.T) {
	pages := make([][]types.Object, 2)
	for i := range pages {
		pages[i] = make([]types.Object, 800)
		for j := range pages[i] {
			pages[i][j] = key(fmt.Sprintf("data/%d-%d.json", i, j))
		}
	}
	api := &deleter{pagedLister: pagedLister{pages: pages}}

	result, err := deletePrefix(context.Background(), api, deleteRequest{
		bucket: "bucket", prefix: "data/", maxEntries: 800,
	})
	if err != nil {
		t.Fatalf("deletePrefix() error = %v", err)
	}

	// A level of any size costs one request per deleteBatch keys, not one per key.
	if len(api.batches) != 2 || api.batches[0] != deleteBatch || api.batches[1] != 600 {
		t.Errorf("batches = %v, want [%d 600]", api.batches, deleteBatch)
	}
	if result.Objects != 1600 {
		t.Errorf("result.Objects = %d, want 1600", result.Objects)
	}
}
