// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// Deletion is what a recursive delete removed.
type Deletion struct {
	Objects int
}

// objectsDeleter removes a batch of keys in one request. It is named separately so deleting can
// be tested without an S3.
type objectsDeleter interface {
	DeleteObjects(
		context.Context,
		*awss3.DeleteObjectsInput,
		...func(*awss3.Options),
	) (*awss3.DeleteObjectsOutput, error)
}

// deleteAPI is what a recursive delete needs: the walk over the keys, and the deletes.
type deleteAPI interface {
	awss3.ListObjectsV2APIClient
	objectsDeleter
}

// deleteBatch is how many keys one request removes. S3 caps a DeleteObjects request at a
// thousand keys, so a level of any size costs one request per thousand rather than one per key.
const deleteBatch = 1000

// DeletePrefix removes every key under prefix, however deep, the folder markers standing for
// the levels included. prefix must name a level — it ends with Delimiter — and the empty prefix,
// which would be the whole bucket, is refused.
//
// Unlike a recursive download it walks under no scanned-keys cap: a delete that stopped at a cap
// would leave the level half removed while reporting it gone, and there is no undo to fall back
// on. What was removed before a failure or a cancellation stays removed, which is why the
// returned Deletion counts it even when an error comes with it.
//
// onDeleted, when non-nil, is called after each batch with how many keys are gone.
//
// On a versioned bucket this writes delete markers rather than removing the data, exactly as
// DeleteObject does: the level disappears from a listing, but its versions remain.
func (c *Client) DeletePrefix(
	ctx context.Context,
	bucket, prefix string,
	onDeleted func(done int),
) (*Deletion, error) {
	return deletePrefix(ctx, c.api, deleteRequest{
		bucket:     bucket,
		prefix:     prefix,
		maxEntries: c.maxEntries,
		onDeleted:  onDeleted,
	})
}

// deleteRequest holds the parameters of one recursive delete, keeping deletePrefix's signature
// readable.
type deleteRequest struct {
	bucket     string
	prefix     string
	maxEntries int32 // keys per listing request
	onDeleted  func(done int)
}

// deletePrefix is DeletePrefix against any page source, so the walk can be tested without an S3.
func deletePrefix(
	ctx context.Context,
	api deleteAPI,
	req deleteRequest,
) (*Deletion, error) {
	result := &Deletion{}

	// A prefix that does not end with the delimiter is a string keys begin with rather than a
	// level: removing everything under "data/2024" would take "data/2024-backup/" with it.
	if req.prefix == "" || !strings.HasSuffix(req.prefix, Delimiter) {
		return result, fmt.Errorf("prefix %q does not name a level", req.prefix)
	}

	// No delimiter: the whole subtree comes back as plain keys, which is what makes the delete
	// recursive.
	paginator := awss3.NewListObjectsV2Paginator(api, &awss3.ListObjectsV2Input{
		Bucket:  aws.String(req.bucket),
		Prefix:  aws.String(req.prefix),
		MaxKeys: aws.Int32(req.maxEntries),
	})

	batch := make([]types.ObjectIdentifier, 0, deleteBatch)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}

		out, err := api.DeleteObjects(ctx, &awss3.DeleteObjectsInput{
			Bucket: aws.String(req.bucket),
			// Quiet: the answer is the keys that failed, and nothing about the ones that went
			// as asked.
			Delete: &types.Delete{Objects: batch, Quiet: aws.Bool(true)},
		})
		if err != nil {
			return err
		}

		// A batch is one request answered per key, so a key S3 refused arrives here rather than
		// as a failed request. The rest of the batch is gone regardless, which is what the count
		// has to reflect.
		sent := len(batch)
		result.Objects += sent - len(out.Errors)
		batch = batch[:0]
		if req.onDeleted != nil {
			req.onDeleted(result.Objects)
		}

		if len(out.Errors) > 0 {
			refused := out.Errors[0]
			return fmt.Errorf(
				"%d of %d keys refused, first %s: %s",
				len(out.Errors),
				sent,
				aws.ToString(refused.Key),
				aws.ToString(refused.Message),
			)
		}
		return nil
	}

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return result, err
		}

		for _, o := range page.Contents {
			batch = append(batch, types.ObjectIdentifier{Key: o.Key})
			if len(batch) < deleteBatch {
				continue
			}
			if err := flush(); err != nil {
				return result, err
			}
		}
	}

	if err := flush(); err != nil {
		return result, err
	}
	return result, nil
}
