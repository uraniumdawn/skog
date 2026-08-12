// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"reflect"
	"testing"
)

// TestPlanOpenedPages verifies the ordering rule behind h/l navigation: a newly
// opened page is inserted immediately after the current page, and re-opening an
// existing page moves it to that same position.
func TestPlanOpenedPages(t *testing.T) {
	tests := []struct {
		name      string
		names     []string
		current   string
		open      string
		wantOrder []string
		wantIndex int
	}{
		{
			name:      "rule: new page inserted after current, tail shifts right",
			names:     []string{"a", "b", "c", "d", "e"},
			current:   "d", // index 3
			open:      "new",
			wantOrder: []string{"a", "b", "c", "d", "new", "e"},
			wantIndex: 4,
		},
		{
			name:      "first page ever",
			names:     []string{},
			current:   "",
			open:      "a",
			wantOrder: []string{"a"},
			wantIndex: 0,
		},
		{
			name:      "open while on last page appends at end",
			names:     []string{"a", "b"},
			current:   "b",
			open:      "c",
			wantOrder: []string{"a", "b", "c"},
			wantIndex: 2,
		},
		{
			// Edge case 1: re-open a page located BEFORE the current one.
			name:      "reopen page before current moves it after current",
			names:     []string{"a", "b", "c", "d", "e"},
			current:   "d", // index 3
			open:      "b", // existing at index 1
			wantOrder: []string{"a", "c", "d", "b", "e"},
			wantIndex: 3,
		},
		{
			name:      "reopen page after current moves it after current",
			names:     []string{"a", "b", "c", "d", "e"},
			current:   "b", // index 1
			open:      "d", // existing at index 3
			wantOrder: []string{"a", "b", "d", "c", "e"},
			wantIndex: 2,
		},
		{
			// Edge case 2: re-opening the current page itself (e.g. a forced
			// refresh). It swaps places with its right-hand neighbour. This
			// documents current behaviour rather than endorsing it.
			name:      "reopen current page swaps with right neighbour",
			names:     []string{"a", "b", "c", "d", "e"},
			current:   "c", // index 2
			open:      "c",
			wantOrder: []string{"a", "b", "d", "c", "e"},
			wantIndex: 3,
		},
		{
			// Edge case 3: front page is not persistent (a modal is in front),
			// so it is not found in the order and the page is appended at the end.
			name:      "current not in list appends at end",
			names:     []string{"a", "b", "c"},
			current:   "modal",
			open:      "new",
			wantOrder: []string{"a", "b", "c", "new"},
			wantIndex: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOrder, gotIndex := planOpenedPages(tt.names, tt.current, tt.open)
			if !reflect.DeepEqual(gotOrder, tt.wantOrder) {
				t.Errorf("order = %v, want %v", gotOrder, tt.wantOrder)
			}
			if gotIndex != tt.wantIndex {
				t.Errorf("index = %d, want %d", gotIndex, tt.wantIndex)
			}
			if gotOrder[gotIndex] != tt.open {
				t.Errorf(
					"order[%d] = %q, want the opened page %q",
					gotIndex,
					gotOrder[gotIndex],
					tt.open,
				)
			}
		})
	}
}

// TestPlanOpenedPages_BackwardReturnsToOpener asserts the core h-navigation
// guarantee: after opening a brand-new page, the row directly before it is the
// page you opened it from, so pressing 'h' returns there.
func TestPlanOpenedPages_BackwardReturnsToOpener(t *testing.T) {
	names := []string{"a", "b", "c", "d", "e"}
	current := "d"

	order, index := planOpenedPages(names, current, "new")
	if index == 0 {
		t.Fatalf("new page landed at index 0; nothing to step back to")
	}
	if got := order[index-1]; got != current {
		t.Errorf("page before the new one = %q, want the opener %q", got, current)
	}
}

func TestIndexOfString(t *testing.T) {
	s := []string{"a", "b", "c"}
	if got := indexOfString(s, "b"); got != 1 {
		t.Errorf("indexOfString(b) = %d, want 1", got)
	}
	if got := indexOfString(s, "x"); got != -1 {
		t.Errorf("indexOfString(x) = %d, want -1", got)
	}
	if got := indexOfString(nil, "a"); got != -1 {
		t.Errorf("indexOfString(nil) = %d, want -1", got)
	}
}
