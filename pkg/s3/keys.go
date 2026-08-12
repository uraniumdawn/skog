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
