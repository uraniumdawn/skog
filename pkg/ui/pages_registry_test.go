// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"testing"

	"github.com/rivo/tview"
)

// TestSetPageNavigation covers what <h> and <l> resolve to: a page registers the levels it
// has, and a rebuild that has fewer levels than the build before it leaves none behind.
func TestSetPageNavigation(t *testing.T) {
	registry := &PagesRegistry{
		Ascend:  make(map[string]func()),
		Descend: make(map[string]func()),
	}

	up, down := 0, 0
	registry.SetPageNavigation("page", func() { up++ }, func() { down++ })

	registry.Ascend["page"]()
	registry.Descend["page"]()
	if up != 1 || down != 1 {
		t.Fatalf("up = %d, down = %d, want 1 and 1", up, down)
	}

	// The viewer of a file that declares no schema: rebuilt with a level above it and none
	// below, where the build before it had both.
	registry.SetPageNavigation("page", func() { up++ }, nil)
	if _, ok := registry.Ascend["page"]; !ok {
		t.Error("the level above the page is gone after a rebuild that kept it")
	}
	if _, ok := registry.Descend["page"]; ok {
		t.Error("a rebuild with no level below the page left the previous one registered")
	}
}

// TestAscendDescend covers the dispatch behind <h> and <l>: they act on the page in front, and
// report false on a page that has no level in that direction so that the keypress is left to
// whatever is in front — a modal answering to its own keys.
func TestAscendDescend(t *testing.T) {
	pages := tview.NewPages()
	pages.AddPage("objects", tview.NewBox(), true, true)
	pages.AddPage("modal", tview.NewBox(), true, false)

	registry := &PagesRegistry{
		UI:      &UI{Pages: pages},
		Ascend:  make(map[string]func()),
		Descend: make(map[string]func()),
	}
	app := &App{Layout: &Layout{PagesRegistry: registry}}

	up, down := 0, 0
	registry.SetPageNavigation("objects", func() { up++ }, func() { down++ })

	if !app.Ascend() || !app.Descend() {
		t.Fatal("the page in front has both levels registered, but they were not reached")
	}
	if up != 1 || down != 1 {
		t.Fatalf("up = %d, down = %d, want 1 and 1", up, down)
	}

	pages.ShowPage("modal")
	pages.SendToFront("modal")
	if app.Ascend() || app.Descend() {
		t.Error("a page with no levels registered claimed the keypress")
	}
	if up != 1 || down != 1 {
		t.Errorf("up = %d, down = %d: the page behind the modal was navigated", up, down)
	}
}
