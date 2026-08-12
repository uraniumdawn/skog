// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package s3

import "testing"

// What the UI shows for a row: the part of the key that belongs to the level being listed.
func TestName(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		full    string
		display string
	}{
		{
			name:    "folder at the root of a bucket",
			prefix:  "",
			full:    "data/",
			display: "data/",
		},
		{
			name:    "folder one level down",
			prefix:  "data/",
			full:    "data/time/",
			display: "time/",
		},
		{
			name:    "object at a deep level",
			prefix:  "data/time/02/",
			full:    "data/time/02/12.json",
			display: "12.json",
		},
		{
			name:    "object at the root of a bucket",
			prefix:  "",
			full:    "12.json",
			display: "12.json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Name(tt.prefix, tt.full); got != tt.display {
				t.Errorf("Name(%q, %q) = %q, want %q", tt.prefix, tt.full, got, tt.display)
			}
		})
	}
}

func TestDisplayPath(t *testing.T) {
	if got := DisplayPath("bucket2", "data/time/"); got != "/bucket2/data/time/" {
		t.Errorf("DisplayPath() = %q, want %q", got, "/bucket2/data/time/")
	}
	if got := DisplayPath("bucket2", ""); got != "/bucket2/" {
		t.Errorf("DisplayPath() = %q, want %q", got, "/bucket2/")
	}
}
