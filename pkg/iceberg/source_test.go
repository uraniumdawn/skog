// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/uraniumdawn/skog/pkg/s3"
)

// fakeSource is an S3 that answers out of maps: a level is the batches registered under its
// prefix, an object is the bytes registered under its key.
type fakeSource struct {
	pages  map[string][]*s3.Listing
	bodies map[string][]byte
	// listed records every prefix asked for, so a test can pin down how many requests reading
	// something costs.
	listed []string
	err    error
}

func newFakeSource() *fakeSource {
	return &fakeSource{
		pages:  make(map[string][]*s3.Listing),
		bodies: make(map[string][]byte),
	}
}

// level registers one batch: the child prefixes and the object keys of a level.
func (f *fakeSource) level(prefix string, prefixes []string, keys ...string) *fakeSource {
	objects := make([]s3.Object, 0, len(keys))
	for i, key := range keys {
		objects = append(objects, s3.Object{
			Key:          key,
			Size:         int64(100 + i),
			LastModified: time.Unix(int64(1_700_000_000+i), 0).UTC(),
		})
	}

	f.pages[prefix] = append(f.pages[prefix], &s3.Listing{
		Prefix:   prefix,
		Prefixes: prefixes,
		Objects:  objects,
	})
	return f
}

// object registers an object's bytes.
func (f *fakeSource) object(key string, body []byte) *fakeSource {
	f.bodies[key] = body
	return f
}

func (f *fakeSource) List(_ context.Context, bucket, prefix, token string) (*s3.Listing, error) {
	if f.err != nil {
		return nil, f.err
	}
	f.listed = append(f.listed, prefix)

	batches := f.pages[prefix]
	index := 0
	if token != "" {
		index, _ = strconv.Atoi(token)
	}
	if index >= len(batches) {
		return &s3.Listing{Bucket: bucket, Prefix: prefix}, nil
	}

	batch := *batches[index]
	batch.Bucket = bucket
	if index+1 < len(batches) {
		batch.NextToken = strconv.Itoa(index + 1)
	}
	return &batch, nil
}

func (f *fakeSource) Get(_ context.Context, _, key string) (io.ReadCloser, error) {
	body, ok := f.bodies[key]
	if !ok {
		return nil, fmt.Errorf("no such key: %s", key)
	}
	return io.NopCloser(bytes.NewReader(body)), nil
}
