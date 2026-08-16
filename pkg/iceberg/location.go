// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"fmt"
	"strings"

	"github.com/uraniumdawn/skog/pkg/s3"
)

// schemes are the URI schemes that name an object in S3. A table written by Spark or Hive
// carries s3a:// or s3n://, which are Hadoop's own names for the same storage.
var schemes = []string{"s3://", "s3a://", "s3n://"}

// ParseLocation splits a path out of table metadata into the bucket and key it names.
//
// A path with no scheme is taken as relative to base, the table's own location: a table at
// "s3://w/db/t" recording a manifest as "metadata/x.avro" holds it at w:db/t/metadata/x.avro.
// A scheme that is not S3 — a table on HDFS or GCS, reachable but not by skog — is refused with
// a message naming it, rather than turned into a key that would be looked for and not found.
func ParseLocation(path, base string) (bucket, key string, err error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return "", "", fmt.Errorf("empty location")
	}

	if bucket, key, ok := splitScheme(trimmed); ok {
		if bucket == "" {
			return "", "", fmt.Errorf("location names no bucket: %s", path)
		}
		return bucket, key, nil
	}

	if i := strings.Index(trimmed, "://"); i > 0 {
		return "", "", fmt.Errorf("%s is not in S3: %s", trimmed[:i], path)
	}

	baseBucket, baseKey, ok := splitScheme(strings.TrimSpace(base))
	if !ok || baseBucket == "" {
		return "", "", fmt.Errorf("relative location %q with no table location to resolve it against", path)
	}
	return baseBucket, join(baseKey, strings.TrimPrefix(trimmed, s3.Delimiter)), nil
}

// join puts a relative path under a prefix, with one delimiter between them.
func join(prefix, rest string) string {
	if prefix == "" {
		return rest
	}
	return strings.TrimSuffix(prefix, s3.Delimiter) + s3.Delimiter + rest
}

// splitScheme splits an s3-style URI into its bucket and key, reporting whether the path carried
// such a scheme at all.
func splitScheme(path string) (bucket, key string, ok bool) {
	for _, scheme := range schemes {
		if !hasPrefixFold(path, scheme) {
			continue
		}

		bucket, key, found := strings.Cut(path[len(scheme):], s3.Delimiter)
		if !found {
			return bucket, "", true
		}
		return bucket, key, true
	}
	return "", "", false
}

// hasPrefixFold reports whether s starts with prefix, ignoring case. Every writer spells a
// scheme in lower case, but a scheme is not case-sensitive.
func hasPrefixFold(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}
