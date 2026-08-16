// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package config

import "testing"

// <Tab> walks the modes from the safest to the least safe and back, and never leaves a profile
// on something skog does not know.
func TestNextMode(t *testing.T) {
	tests := []struct {
		from Mode
		want Mode
	}{
		{from: ReadOnly, want: Regular},
		{from: Regular, want: Yolo},
		{from: Yolo, want: ReadOnly},
		{from: Mode("read_only"), want: Yolo}, // unknown counts as regular
		{from: Mode(""), want: Yolo},
	}

	for _, tt := range tests {
		if got := NextMode(tt.from); got != tt.want {
			t.Errorf("NextMode(%q) = %q, want %q", tt.from, got, tt.want)
		}
	}
}

func TestNextModeCyclesThroughEveryMode(t *testing.T) {
	seen := map[Mode]bool{}
	mode := Regular
	for range modeCycle {
		mode = NextMode(mode)
		seen[mode] = true
	}
	for _, want := range []Mode{ReadOnly, Regular, Yolo} {
		if !seen[want] {
			t.Errorf("%q is unreachable with <Tab>", want)
		}
	}
}

func TestSetProfileMode(t *testing.T) {
	cfg := &Config{}

	// Regular is the default, so it is stored as no entry at all rather than as a value.
	cfg.SetProfileMode("local", Regular)
	if len(cfg.Skog.Modes) != 0 {
		t.Errorf("modes = %v, want the default stored as nothing", cfg.Skog.Modes)
	}

	cfg.SetProfileMode("local", Yolo)
	if got := cfg.ProfileMode("local"); got != Yolo {
		t.Errorf("mode = %q, want %q", got, Yolo)
	}

	// Switching back to the default clears the entry instead of leaving it behind.
	cfg.SetProfileMode("local", Regular)
	if _, ok := cfg.Skog.Modes["local"]; ok {
		t.Errorf("modes = %v, want the entry dropped", cfg.Skog.Modes)
	}
	if got := cfg.ProfileMode("local"); got != Regular {
		t.Errorf("mode = %q, want %q", got, Regular)
	}

	// An unknown mode is refused rather than stored, so nothing can pin a profile to it.
	cfg.SetProfileMode("local", Mode("readonly"))
	if got := cfg.ProfileMode("local"); got != Regular {
		t.Errorf("mode = %q, want %q", got, Regular)
	}
}
