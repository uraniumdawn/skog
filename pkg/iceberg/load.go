// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"context"
	"io"
)

// LoadMetadata reads a table metadata document out of S3.
func LoadMetadata(ctx context.Context, src Source, bucket, key string) (*Metadata, error) {
	return readObject(ctx, src, bucket, key, ParseMetadata)
}

// LoadManifestList reads a snapshot's manifest list out of S3.
func LoadManifestList(
	ctx context.Context,
	src Source,
	bucket, key string,
) ([]ManifestFile, error) {
	return readObject(ctx, src, bucket, key, ReadManifestList)
}

// LoadManifest reads one manifest out of S3. inherited is the manifest's sequence number as its
// manifest list records it; see ReadManifest.
func LoadManifest(
	ctx context.Context,
	src Source,
	bucket, key string,
	inherited int64,
) (*Manifest, error) {
	return readObject(ctx, src, bucket, key, func(r io.Reader) (*Manifest, error) {
		return ReadManifest(r, inherited)
	})
}
