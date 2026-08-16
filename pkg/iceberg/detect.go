// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package iceberg

import (
	"bufio"
	"context"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uraniumdawn/skog/pkg/s3"
)

const (
	// MetadataDir is the level of a table that holds its metadata documents, its manifest lists
	// and its manifests.
	MetadataDir = "metadata" + s3.Delimiter
	// metadataSuffix is what a table metadata document ends in, whether it is named v3.metadata.json
	// or 00003-<uuid>.metadata.json.
	metadataSuffix = ".metadata.json"
	// versionHint is the file a hadoop-catalog table writes the current version number into. It
	// is the only pointer at a current version that lives in the bucket; every other catalog
	// keeps it outside.
	versionHint = "version-hint.text"
	// versionHintMax bounds the read of that file: it holds a number, and anything longer is not
	// one.
	versionHintMax = 64
)

// IsTable reports whether prefix names an Iceberg table: it holds a metadata level, and that
// level holds at least one table metadata document.
//
// Both halves are needed. A prefix with a folder called metadata is not a table, and a table
// under migration may have a metadata level holding manifests before it holds a document.
func IsTable(ctx context.Context, src Source, bucket, prefix string) (bool, error) {
	found := false

	_, err := eachPage(ctx, src, bucket, prefix+MetadataDir, 0, func(listing *s3.Listing) bool {
		for _, object := range listing.Objects {
			if strings.HasSuffix(object.Key, metadataSuffix) {
				found = true
				return false
			}
		}
		return true
	})
	if err != nil {
		return false, err
	}
	return found, nil
}

// Marks a metadata document as the one a reader would open.
const (
	// MarkHint is the version version-hint.text names. It is what the writer committed.
	MarkHint = "hint"
	// MarkLatest is the highest version number present, and is a guess: without a catalog and
	// without a hint file, the newest document on the shelf is all there is to go on.
	MarkLatest = "latest"
)

// Version is one table metadata document.
type Version struct {
	// Key is the full S3 key; File is its name alone.
	Key  string
	File string
	// Number is the version the name carries, -1 for a name that carries none.
	Number   int
	Size     int64
	Modified time.Time
	// Mark is MarkHint or MarkLatest for the document a reader would open, empty for the rest.
	Mark string
}

// Versions lists a table's metadata documents, newest first.
//
// Which one is current is not in the bucket unless the table keeps a version hint there: every
// other catalog holds that pointer itself. So the newest is marked rather than opened, and the
// mark says on what basis — see MarkHint and MarkLatest.
//
// maxKeys caps how many keys the metadata level is walked for, zero for no cap; truncated
// reports that it stopped early, and so that a newer document may exist and not be listed.
func Versions(
	ctx context.Context,
	src Source,
	bucket, prefix string,
	maxKeys int,
) (versions []Version, truncated bool, err error) {
	metadata := prefix + MetadataDir

	truncated, err = eachPage(ctx, src, bucket, metadata, maxKeys, func(listing *s3.Listing) bool {
		for _, object := range listing.Objects {
			if !strings.HasSuffix(object.Key, metadataSuffix) {
				continue
			}
			name := path.Base(object.Key)
			versions = append(versions, Version{
				Key:      object.Key,
				File:     name,
				Number:   versionNumber(name),
				Size:     object.Size,
				Modified: object.LastModified,
			})
		}
		return true
	})
	if err != nil {
		return nil, false, err
	}

	sortVersions(versions)
	mark(ctx, src, bucket, metadata, versions)

	return versions, truncated, nil
}

// sortVersions orders the documents newest first: by the version their name carries, and by
// when they were written where a name carries none.
func sortVersions(versions []Version) {
	sort.SliceStable(versions, func(i, j int) bool {
		if versions[i].Number != versions[j].Number {
			return versions[i].Number > versions[j].Number
		}
		return versions[i].Modified.After(versions[j].Modified)
	})
}

// mark points at the document a reader would open: the one the version hint names, or the
// highest-numbered one where there is no hint.
func mark(ctx context.Context, src Source, bucket, metadata string, versions []Version) {
	if len(versions) == 0 {
		return
	}

	if hinted, ok := hintedVersion(ctx, src, bucket, metadata); ok {
		for i := range versions {
			if versions[i].Number == hinted {
				versions[i].Mark = MarkHint
				return
			}
		}
	}

	// The list is already newest first, so the first entry is the highest version there is —
	// unless no name carried one at all, in which case there is nothing to call latest.
	if versions[0].Number >= 0 {
		versions[0].Mark = MarkLatest
	}
}

// hintedVersion reads the version number out of version-hint.text, reporting whether the table
// keeps one. A table without the file is every table under a real catalog, so a failed read is
// an absent hint and not an error.
func hintedVersion(ctx context.Context, src Source, bucket, metadata string) (int, bool) {
	number, err := readObject(ctx, src, bucket, metadata+versionHint, func(r io.Reader) (int, error) {
		line, err := bufio.NewReader(io.LimitReader(r, versionHintMax)).ReadString('\n')
		if err != nil && err != io.EOF {
			return 0, err
		}
		return strconv.Atoi(strings.TrimSpace(line))
	})
	if err != nil {
		return 0, false
	}
	return number, true
}

// versionNumber is the version a metadata document's name carries, -1 for a name that carries
// none.
//
// Two spellings are in use: v3.metadata.json, which a hadoop table writes, and
// 00003-<uuid>.metadata.json, which every other writer produces. Either may carry a compression
// suffix — 00003-<uuid>.gz.metadata.json — which is left where it is: only the leading number
// is being read.
func versionNumber(name string) int {
	trimmed := strings.TrimPrefix(name, "v")

	end := 0
	for end < len(trimmed) && trimmed[end] >= '0' && trimmed[end] <= '9' {
		end++
	}
	if end == 0 {
		return -1
	}

	number, err := strconv.Atoi(trimmed[:end])
	if err != nil {
		return -1
	}
	return number
}
