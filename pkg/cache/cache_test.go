// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// write is a fetch that puts content at dst, standing in for a download.
func write(content string) func(string) error {
	return func(dst string) error {
		return os.WriteFile(dst, []byte(content), 0o644)
	}
}

// age backdates an entry so Trim has an order to work with: the tests need
// mtimes further apart than a filesystem's resolution.
func age(t *testing.T, path string, d time.Duration) {
	t.Helper()
	when := time.Now().Add(-d)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatalf("backdating %s: %v", path, err)
	}
}

func newCache(t *testing.T, maxBytes int64) *Cache {
	t.Helper()
	c, err := New(t.TempDir(), maxBytes)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func TestNewCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "skog")
	if _, err := New(dir, 1<<20); err != nil {
		t.Fatalf("New: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, entriesDir))
	if err != nil {
		t.Fatalf("entries dir not created: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("entries path is not a directory")
	}
}

func TestPathIsStableAndDistinct(t *testing.T) {
	c := newCache(t, 1<<20)

	base := c.Path("default", "bucket", "a/b.parquet", "etag1")
	if got := c.Path("default", "bucket", "a/b.parquet", "etag1"); got != base {
		t.Errorf("Path is not stable: %s != %s", got, base)
	}

	// A key is arbitrary text and may hold anything; none of it may reach the filesystem.
	for _, other := range []string{
		c.Path("other", "bucket", "a/b.parquet", "etag1"),
		c.Path("default", "other", "a/b.parquet", "etag1"),
		c.Path("default", "bucket", "a/c.parquet", "etag1"),
		c.Path("default", "bucket", "a/b.parquet", "etag2"),
	} {
		if other == base {
			t.Errorf("distinct inputs share a path: %s", other)
		}
	}

	if dir := filepath.Dir(base); dir != filepath.Join(c.dir, entriesDir) {
		t.Errorf("entry outside the entries dir: %s", dir)
	}
}

// A key holding path separators or "..": the hash is what lands on disk, so an entry
// can never escape the cache directory the way a downloaded key could.
func TestPathContainsNoKeyText(t *testing.T) {
	c := newCache(t, 1<<20)

	path := c.Path("default", "bucket", "../../etc/passwd", "etag")
	if name := filepath.Base(path); name == "passwd" {
		t.Fatal("key text reached the filename")
	}
	if filepath.Dir(path) != filepath.Join(c.dir, entriesDir) {
		t.Fatalf("entry escaped the entries dir: %s", path)
	}
}

func TestGetMissThenHit(t *testing.T) {
	c := newCache(t, 1<<20)
	path := c.Path("default", "bucket", "key", "etag")

	if c.Get(path) {
		t.Fatal("hit on an empty cache")
	}

	if err := c.Put(path, write("body")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !c.Get(path) {
		t.Fatal("miss on an entry just written")
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the entry: %v", err)
	}
	if string(body) != "body" {
		t.Errorf("entry holds %q, want %q", body, "body")
	}
}

// An ETag change is what makes a changed object a miss, so nothing else has to check staleness.
func TestChangedETagMisses(t *testing.T) {
	c := newCache(t, 1<<20)

	first := c.Path("default", "bucket", "key", "etag1")
	if err := c.Put(first, write("old")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if c.Get(c.Path("default", "bucket", "key", "etag2")) {
		t.Fatal("a changed ETag hit the cache")
	}
}

// Get touches the entry, since mtime is what Trim evicts by.
func TestGetTouches(t *testing.T) {
	c := newCache(t, 1<<20)
	path := c.Path("default", "bucket", "key", "etag")
	if err := c.Put(path, write("body")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	age(t, path, time.Hour)

	before := modTime(t, path)
	if !c.Get(path) {
		t.Fatal("miss on an entry just written")
	}

	if !modTime(t, path).After(before) {
		t.Error("Get did not touch the entry")
	}
}

func TestPutFailureLeavesNothing(t *testing.T) {
	c := newCache(t, 1<<20)
	path := c.Path("default", "bucket", "key", "etag")

	sentinel := errors.New("download failed")
	err := c.Put(path, func(dst string) error {
		// A partial body, as a cancelled download would leave.
		if err := os.WriteFile(dst, []byte("half"), 0o644); err != nil {
			return err
		}
		return sentinel
	})

	if !errors.Is(err, sentinel) {
		t.Fatalf("Put returned %v, want %v", err, sentinel)
	}
	if c.Get(path) {
		t.Error("a failed Put left an entry behind")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a failed Put left a file behind")
	}
}

func TestTrimEvictsOldestFirst(t *testing.T) {
	// Room for two entries of ten bytes, not three.
	c := newCache(t, 25)

	oldest := c.Path("default", "bucket", "oldest", "e")
	middle := c.Path("default", "bucket", "middle", "e")
	newest := c.Path("default", "bucket", "newest", "e")

	for _, e := range []struct {
		path string
		age  time.Duration
	}{
		{oldest, 3 * time.Hour},
		{middle, 2 * time.Hour},
		{newest, time.Hour},
	} {
		if err := c.Put(e.path, write("0123456789")); err != nil {
			t.Fatalf("Put: %v", err)
		}
		age(t, e.path, e.age)
	}

	if err := c.Trim(); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	if _, err := os.Stat(oldest); !os.IsNotExist(err) {
		t.Error("the oldest entry survived")
	}
	for _, path := range []string{middle, newest} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("a newer entry was evicted: %s", path)
		}
	}

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size > 25 {
		t.Errorf("cache holds %d bytes, over the 25 byte budget", size)
	}
}

// Put trims, so the budget holds without anything else having to call Trim.
func TestPutTrims(t *testing.T) {
	c := newCache(t, 15)

	first := c.Path("default", "bucket", "first", "e")
	if err := c.Put(first, write("0123456789")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	age(t, first, time.Hour)

	second := c.Path("default", "bucket", "second", "e")
	if err := c.Put(second, write("0123456789")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := os.Stat(first); !os.IsNotExist(err) {
		t.Error("the older entry survived a Put over the budget")
	}
}

// The entry just written is what the caller is about to read, so it is never the one evicted
// — even when it alone is over the budget.
func TestPutKeepsWhatItWrote(t *testing.T) {
	c := newCache(t, 5)
	path := c.Path("default", "bucket", "key", "etag")

	if err := c.Put(path, write("0123456789")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Put evicted the entry it had just written: %v", err)
	}
}

func TestClear(t *testing.T) {
	c := newCache(t, 1<<20)
	for _, name := range []string{"a", "b"} {
		if err := c.Put(c.Path("default", "bucket", name, "e"), write("01234")); err != nil {
			t.Fatalf("Put: %v", err)
		}
	}

	freed, err := c.Clear()
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if freed != 10 {
		t.Errorf("Clear freed %d bytes, want 10", freed)
	}

	size, err := c.Size()
	if err != nil {
		t.Fatalf("Size: %v", err)
	}
	if size != 0 {
		t.Errorf("cache holds %d bytes after Clear", size)
	}

	// The cache stays usable: Clear empties it rather than tearing it down.
	if err := c.Put(c.Path("default", "bucket", "c", "e"), write("x")); err != nil {
		t.Errorf("Put after Clear: %v", err)
	}
}

// A zero budget lifts the cap, the way s3.max_scanned_keys does.
func TestUnlimitedBudgetNeverEvicts(t *testing.T) {
	c := newCache(t, 0)

	first := c.Path("default", "bucket", "first", "e")
	if err := c.Put(first, write("0123456789")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	age(t, first, time.Hour)
	if err := c.Put(c.Path("default", "bucket", "second", "e"), write("0123456789")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	if _, err := os.Stat(first); err != nil {
		t.Error("an unlimited cache evicted an entry")
	}
}

func modTime(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.ModTime()
}
