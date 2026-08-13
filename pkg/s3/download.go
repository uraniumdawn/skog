// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// Download is what a download transferred.
type Download struct {
	Objects int
	Bytes   int64
	// Path is where it landed: the file for a single object, the folder for a prefix.
	Path string
	// Partial reports that the walk stopped at the scanned-keys cap, so only part of what the
	// prefix holds was fetched.
	Partial bool
}

// objectGetter reads an object's body. It is the only S3 call a single download makes, and is
// named separately so downloading can be tested without an S3.
type objectGetter interface {
	GetObject(
		context.Context,
		*awss3.GetObjectInput,
		...func(*awss3.Options),
	) (*awss3.GetObjectOutput, error)
}

// downloadAPI is what a recursive download needs: the walk over the keys, and the reads.
type downloadAPI interface {
	awss3.ListObjectsV2APIClient
	objectGetter
}

// DownloadObject writes one object into dir, as dir/<bucket>/<key>. A file already there is
// overwritten.
func (c *Client) DownloadObject(ctx context.Context, bucket, key, dir string) (*Download, error) {
	// A key ending in the delimiter is a folder marker, and would be written as a file where the
	// folder it stands for has to go. DownloadPrefix is what fetches a level.
	if key == "" || strings.HasSuffix(key, Delimiter) {
		return nil, fmt.Errorf("key %q names a level, not an object", key)
	}

	path, err := localPath(dir, bucket, key)
	if err != nil {
		return nil, err
	}

	size, err := downloadObject(ctx, c.api, bucket, key, path)
	if err != nil {
		return nil, err
	}
	return &Download{Objects: 1, Bytes: size, Path: path}, nil
}

// DownloadPrefix writes every key under prefix, however deep, into dir, keeping the hierarchy
// the keys describe: dir/<bucket>/<key>. prefix is empty to download a whole bucket.
//
// onObject, when non-nil, is called after each object with how many are done and the key that
// finished, since a prefix takes as many requests as it has keys and is worth reporting on.
//
// The walk stops at the client's scanned-keys cap; see Download.Partial. What was written up to
// a failure or a cancellation stays on disk — only the object in flight is discarded.
func (c *Client) DownloadPrefix(
	ctx context.Context,
	bucket, prefix, dir string,
	onObject func(done int, key string),
) (*Download, error) {
	return downloadPrefix(ctx, c.api, downloadRequest{
		bucket:     bucket,
		prefix:     prefix,
		dir:        dir,
		maxEntries: c.maxEntries,
		maxScanned: c.maxScannedKeys,
		onObject:   onObject,
	})
}

// downloadRequest holds the parameters of one recursive download, keeping downloadPrefix's
// signature readable.
type downloadRequest struct {
	bucket     string
	prefix     string
	dir        string
	maxEntries int32 // keys per request
	maxScanned int   // zero means no cap
	onObject   func(done int, key string)
}

// downloadPrefix is DownloadPrefix against any page source, so the walk can be tested without
// an S3.
func downloadPrefix(
	ctx context.Context,
	api downloadAPI,
	req downloadRequest,
) (*Download, error) {
	root, err := localPath(req.dir, req.bucket, req.prefix)
	if err != nil {
		return nil, err
	}
	result := &Download{Path: root}

	// No delimiter: the whole subtree comes back as plain keys, which is what makes the download
	// recursive.
	paginator := awss3.NewListObjectsV2Paginator(api, &awss3.ListObjectsV2Input{
		Bucket:  aws.String(req.bucket),
		Prefix:  aws.String(req.prefix),
		MaxKeys: aws.Int32(req.maxEntries),
	})

	scanned := 0
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, err
		}

		for _, o := range page.Contents {
			key := aws.ToString(o.Key)
			scanned++

			// A key ending in the delimiter is a folder marker — a zero-byte object standing for
			// a level — and is the hierarchy itself rather than content in it.
			if strings.HasSuffix(key, Delimiter) {
				continue
			}

			path, err := localPath(req.dir, req.bucket, key)
			if err != nil {
				return nil, err
			}
			size, err := downloadObject(ctx, api, req.bucket, key, path)
			if err != nil {
				return nil, fmt.Errorf("downloading %s: %w", key, err)
			}

			result.Objects++
			result.Bytes += size
			if req.onObject != nil {
				req.onObject(result.Objects, key)
			}
		}

		if req.maxScanned > 0 && scanned >= req.maxScanned {
			// Hitting the cap on the last page means the prefix happened to end there, not that
			// anything was left out.
			result.Partial = paginator.HasMorePages()
			break
		}
	}

	return result, nil
}

// downloadObject writes one object's body to path, creating the folders leading to it.
//
// The body goes to a temporary file next to the target and is renamed into place once it is
// through: a failed or cancelled download must not leave half an object where the whole one is
// expected.
func downloadObject(
	ctx context.Context,
	api objectGetter,
	bucket, key, path string,
) (int64, error) {
	out, err := api.GetObject(ctx, &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return 0, err
	}
	defer func() { _ = out.Body.Close() }()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.part")
	if err != nil {
		return 0, err
	}
	// A no-op once the file has been renamed into place.
	defer func() { _ = os.Remove(tmp.Name()) }()

	size, err := io.Copy(tmp, out.Body)
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return 0, err
	}

	if err := os.Rename(tmp.Name(), path); err != nil {
		return 0, err
	}
	return size, nil
}

// localPath is the file a key is written to: dir/<bucket>/<key>, the key's delimiters becoming
// path separators.
//
// A key is arbitrary text and may hold "..", so joining one blindly would let a listing write
// anywhere on disk; such a key is refused rather than written somewhere else.
func localPath(dir, bucket, key string) (string, error) {
	root := filepath.Join(dir, bucket)
	path := filepath.Join(root, filepath.FromSlash(key))

	if path != root && !strings.HasPrefix(path, root+string(filepath.Separator)) {
		return "", fmt.Errorf("key %q would be written outside %s", key, root)
	}
	return path, nil
}
