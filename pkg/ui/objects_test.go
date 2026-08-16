// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import "testing"

// A deleted object leaves the page without a refetch, so the rows it leaves behind — folders
// included — must be exactly the ones S3 still holds.
func TestWithoutKey(t *testing.T) {
	rows := []*objectRow{
		{name: "logs/", full: "a/logs/", folder: true},
		{name: "b.json", full: "a/b.json"},
		{name: "c.json", full: "a/c.json"},
	}

	got := withoutKey(rows, "a/b.json")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].full != "a/logs/" || got[1].full != "a/c.json" {
		t.Errorf("rows = %q, %q, want a/logs/, a/c.json", got[0].full, got[1].full)
	}

	// The row a full key names, not the one its display name does: a folder marker object and
	// its prefix share a name.
	if got := withoutKey(rows, "logs/"); len(got) != 3 {
		t.Errorf("len = %d after deleting a display name, want the rows untouched", len(got))
	}
	if got := withoutKey(rows, "a/missing.json"); len(got) != 3 {
		t.Errorf("len = %d after deleting an absent key, want the rows untouched", len(got))
	}
	if got := withoutKey(nil, "a/b.json"); got != nil {
		t.Errorf("withoutKey(nil) = %v, want nil", got)
	}
}
