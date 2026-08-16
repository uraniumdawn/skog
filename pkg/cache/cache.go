// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package cache holds object bodies on disk, so viewing an object a second time costs no
// transfer.
//
// It is a cache of bytes, unlike the page cache in pkg/ui, which remembers which pages are
// open. What is kept here is whole objects: the viewers parse a file, and parquet reads its
// footer before anything else, so a body has to be somewhere seekable before it can be read.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// entriesDir holds the bodies, under the cache directory. Keeping them in a directory of their
// own leaves room beside it for anything else the cache comes to store, and means Clear can
// empty it without touching what it did not write.
const entriesDir = "objects"

// Unlimited is the budget that lifts the cap on the cache's size, as it does for scanned keys.
const Unlimited int64 = 0

// Cache is a size-bounded store of object bodies.
//
// An entry is named for the object it holds: the profile, bucket, key and ETag it was fetched
// with. The ETag being part of the name is what keeps the cache honest — a changed object
// names a different entry, and so is a miss rather than a stale hit, and nothing has to check
// for staleness separately.
//
// Entries are evicted oldest first once the total is over the budget, oldest meaning least
// recently used: a hit touches the entry it found.
type Cache struct {
	dir      string
	maxBytes int64
	// mu serializes eviction against the writes and hits that trigger it. Only one job runs at
	// a time in the UI, but the cache does not depend on that.
	mu sync.Mutex
}

// New opens the cache under dir, creating it if this is the first run. A maxBytes of Unlimited
// lifts the size cap.
func New(dir string, maxBytes int64) (*Cache, error) {
	if err := os.MkdirAll(filepath.Join(dir, entriesDir), 0o755); err != nil {
		return nil, fmt.Errorf("creating the cache directory: %w", err)
	}
	return &Cache{dir: dir, maxBytes: maxBytes}, nil
}

// Path is where the given object is cached, whether or not it is there yet. It is the handle
// every other method takes.
//
// The name is a digest rather than the key itself: an S3 key is arbitrary text, may hold path
// separators or "..", and is long enough to overrun a filename. Hashing it means no part of a
// key reaches the filesystem, so an entry cannot land outside the cache directory — the
// problem s3.localPath has to refuse a key over.
func (c *Cache) Path(profile, bucket, key, etag string) string {
	// A separator no field can hold, so distinct objects cannot share a digest by their fields
	// running together.
	sum := sha256.Sum256([]byte(profile + "\x00" + bucket + "\x00" + key + "\x00" + etag))
	return filepath.Join(c.dir, entriesDir, hex.EncodeToString(sum[:]))
}

// Get reports whether the entry is present, touching it so that eviction sees it as used.
func (c *Cache) Get(path string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if _, err := os.Stat(path); err != nil {
		return false
	}

	now := time.Now()
	if err := os.Chtimes(path, now, now); err != nil {
		// A touch that fails costs the entry its place in the eviction order, which is worth
		// no more than a warning: the body is there and can be read.
		return true
	}
	return true
}

// Put fills the entry by calling fetch with the path to write, then brings the cache back
// under its budget.
//
// A fetch that fails leaves nothing behind: a cancelled or failed download must not be read
// later as if it were the whole object.
func (c *Cache) Put(path string, fetch func(dst string) error) error {
	if err := fetch(path); err != nil {
		_ = os.Remove(path)
		return err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// The entry just written is what the caller is about to read, so it is not a candidate for
	// eviction — not even when it alone is over the budget.
	return c.trim(path)
}

// Trim evicts entries, oldest first, until the cache is within its budget.
func (c *Cache) Trim() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.trim("")
}

// trim evicts everything but keep, oldest first, until the total is within the budget. It is
// called with the lock held.
func (c *Cache) trim(keep string) error {
	if c.maxBytes == Unlimited {
		return nil
	}

	entries, total, err := c.entries()
	if err != nil {
		return err
	}
	if total <= c.maxBytes {
		return nil
	}

	// Oldest first: mtime is the last use, since Get touches what it finds.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].modTime.Before(entries[j].modTime)
	})

	for _, e := range entries {
		if total <= c.maxBytes {
			break
		}
		if e.path == keep {
			continue
		}
		if err := os.Remove(e.path); err != nil {
			// Removing an entry another skog is reading, or has already removed, is not a
			// reason to fail the read this trim is part of.
			continue
		}
		total -= e.size
	}

	return nil
}

// Size is how much the cache currently holds, in bytes.
func (c *Cache) Size() (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	_, total, err := c.entries()
	return total, err
}

// Clear empties the cache and reports how many bytes that freed. The cache stays usable.
func (c *Cache) Clear() (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entries, _, err := c.entries()
	if err != nil {
		return 0, err
	}

	// What was freed is counted as it goes rather than taken from the total: an entry that
	// cannot be removed was not freed, and the figure reported should say so.
	freed := int64(0)
	for _, e := range entries {
		if err := os.Remove(e.path); err != nil {
			continue
		}
		freed += e.size
	}

	return freed, nil
}

// entry is one cached body, as eviction sees it.
type entry struct {
	path    string
	size    int64
	modTime time.Time
}

// entries lists what the cache holds and what it comes to in total. It is called with the lock
// held.
func (c *Cache) entries() ([]entry, int64, error) {
	dir := filepath.Join(c.dir, entriesDir)
	listed, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, fmt.Errorf("reading the cache directory: %w", err)
	}

	entries := make([]entry, 0, len(listed))
	total := int64(0)
	for _, e := range listed {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			// An entry that went between the listing and the stat is simply no longer there.
			continue
		}
		entries = append(entries, entry{
			path:    filepath.Join(dir, e.Name()),
			size:    info.Size(),
			modTime: info.ModTime(),
		})
		total += info.Size()
	}

	return entries, total, nil
}
