// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import "strings"

// Name returns the display name of a full key or prefix at the level given by prefix:
// Name("data/", "data/time/") is "time/" and Name("data/", "data/12.json") is "12.json".
// A full value that does not start with prefix is returned unchanged.
func Name(prefix, full string) string {
	return strings.TrimPrefix(full, prefix)
}

// DisplayPath renders a bucket and prefix the way the user thinks of them, as an absolute
// path: DisplayPath("bucket2", "data/time/") is "/bucket2/data/time/".
func DisplayPath(bucket, prefix string) string {
	return Delimiter + bucket + Delimiter + prefix
}

// Parent returns the level above a key or prefix, and whether there is one: Parent("data/time/")
// is "data/", Parent("data/12.json") is "data/" and Parent("data/") is the root of the bucket,
// the empty prefix. The root itself has nothing above it, which is what the false reports.
//
// It is computed from the key alone rather than from the pages that were opened to reach it, so
// a level reached by any route knows what is above it.
func Parent(key string) (string, bool) {
	if key == "" {
		return "", false
	}

	// A prefix ends with the delimiter, and it is the one before that which separates it from
	// the level above.
	trimmed := strings.TrimSuffix(key, Delimiter)
	i := strings.LastIndex(trimmed, Delimiter)
	if i < 0 {
		return "", true
	}
	return trimmed[:i+1], true
}
