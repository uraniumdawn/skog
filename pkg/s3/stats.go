// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/uraniumdawn/skog/pkg/util"
)

// PrefixStats is the recursive aggregate of everything stored under a prefix.
//
// S3 keeps no such figures: a prefix is only the leading part of a key, so every field here comes
// from walking the keys themselves.
type PrefixStats struct {
	Objects        int
	Size           int64
	LastModified   time.Time
	StorageClasses []string // distinct, sorted
	// Partial reports that the walk stopped at the scanned-keys cap, so the aggregate covers only
	// part of what the prefix holds.
	Partial bool
}

// mixedStorageClass is shown when the keys under a prefix do not share one storage class.
const mixedStorageClass = "mixed"

// StorageClass renders the storage class of the aggregate: the common one when the keys agree.
func (s *PrefixStats) StorageClass() string {
	switch len(s.StorageClasses) {
	case 0:
		return "-"
	case 1:
		return s.StorageClasses[0]
	default:
		return mixedStorageClass
	}
}

// StatPrefix walks every key under prefix, however deep, and aggregates them. prefix is empty to
// aggregate a whole bucket.
//
// onPage, when non-nil, is called after each page with the number of keys scanned so far: a walk
// takes as many requests as the prefix has thousands of keys, and is worth reporting on.
//
// The walk stops at the client's scanned-keys cap; see PrefixStats.Partial.
func (c *Client) StatPrefix(
	ctx context.Context,
	bucket, prefix string,
	onPage func(int),
) (*PrefixStats, error) {
	return statPrefix(ctx, c.api, statRequest{
		bucket:     bucket,
		prefix:     prefix,
		maxEntries: c.maxEntries,
		maxScanned: c.maxScannedKeys,
		onPage:     onPage,
	})
}

// statRequest holds the parameters of one walk, keeping statPrefix's signature readable.
type statRequest struct {
	bucket     string
	prefix     string
	maxEntries int32 // keys per request
	maxScanned int   // zero means no cap
	onPage     func(int)
}

// statPrefix is StatPrefix against any page source, so the walk can be tested without an S3.
func statPrefix(
	ctx context.Context,
	lister awss3.ListObjectsV2APIClient,
	req statRequest,
) (*PrefixStats, error) {
	stats := &PrefixStats{}
	classes := make(map[string]struct{})

	// No delimiter: the whole subtree comes back as plain keys, which is what makes the aggregate
	// recursive.
	paginator := awss3.NewListObjectsV2Paginator(lister, &awss3.ListObjectsV2Input{
		Bucket:  aws.String(req.bucket),
		Prefix:  aws.String(req.prefix),
		MaxKeys: aws.Int32(req.maxEntries),
	})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, o := range page.Contents {
			key := aws.ToString(o.Key)
			// The prefix itself shows up as a zero-byte key when it was created as a folder
			// marker; it is the level being aggregated, not content in it.
			if key == req.prefix {
				continue
			}

			stats.Objects++
			stats.Size += aws.ToInt64(o.Size)
			if modified := aws.ToTime(o.LastModified); modified.After(stats.LastModified) {
				stats.LastModified = modified
			}
			if class := string(o.StorageClass); class != "" {
				classes[class] = struct{}{}
			}
		}

		if req.onPage != nil {
			req.onPage(stats.Objects)
		}

		if req.maxScanned > 0 && stats.Objects >= req.maxScanned {
			// Hitting the cap on the last page means the prefix happened to end there, not that
			// anything was left out.
			stats.Partial = paginator.HasMorePages()
			break
		}
	}

	if len(classes) > 0 {
		stats.StorageClasses = util.SortedKeys(classes)
	}
	return stats, nil
}
