// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"context"
	"io"

	"github.com/uraniumdawn/skog/pkg/s3"
)

// Source is what this package needs of S3: one level of a bucket's keys at a time, and an
// object's bytes. It is an interface so that reading a table can be tested without an S3, and
// so that nothing here has to know how a client is built.
type Source interface {
	// List returns one batch of the level under prefix. token is empty for the first batch and
	// otherwise continues where the batch before it stopped.
	List(ctx context.Context, bucket, prefix, token string) (*s3.Listing, error)
	// Get opens an object's body. It is the caller's to close.
	Get(ctx context.Context, bucket, key string) (io.ReadCloser, error)
}

// From adapts an S3 client to a Source.
func From(client *s3.Client) Source {
	return clientSource{client: client}
}

type clientSource struct {
	client *s3.Client
}

func (s clientSource) List(
	ctx context.Context,
	bucket, prefix, token string,
) (*s3.Listing, error) {
	if token == "" {
		return s.client.ListObjects(ctx, bucket, prefix)
	}
	return s.client.ListMore(ctx, bucket, prefix, token)
}

func (s clientSource) Get(ctx context.Context, bucket, key string) (io.ReadCloser, error) {
	return s.client.GetObject(ctx, bucket, key)
}

// eachPage hands fn one batch of a level at a time until the level ends, fn asks to stop, or
// maxKeys entries have been seen; maxKeys of zero lifts that cap. It reports whether the level
// had more to give than was looked at, so a caller can say so rather than present a part as a
// whole.
func eachPage(
	ctx context.Context,
	src Source,
	bucket, prefix string,
	maxKeys int,
	fn func(*s3.Listing) bool,
) (truncated bool, err error) {
	seen := 0
	token := ""

	for {
		listing, err := src.List(ctx, bucket, prefix, token)
		if err != nil {
			return false, err
		}

		seen += listing.Total()
		if !fn(listing) {
			return false, nil
		}

		if listing.NextToken == "" {
			return false, nil
		}
		if maxKeys > 0 && seen >= maxKeys {
			return true, nil
		}
		token = listing.NextToken
	}
}

// readObject hands the body of an object to read, closing it afterwards. Every file this
// package parses is read this way, so that no reader is left open on a parse that failed.
func readObject[T any](
	ctx context.Context,
	src Source,
	bucket, key string,
	read func(io.Reader) (T, error),
) (T, error) {
	var zero T

	body, err := src.Get(ctx, bucket, key)
	if err != nil {
		return zero, err
	}
	defer func() { _ = body.Close() }()

	return read(body)
}
