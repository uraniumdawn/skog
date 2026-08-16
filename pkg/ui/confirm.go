// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"github.com/gdamore/tcell/v2"

	"github.com/uraniumdawn/skog/pkg/config"
)

// confirmation is the question standing in the status line, waiting for an answer.
type confirmation struct {
	onYes func()
}

// Mode returns the mode the selected profile is worked with, which is what every modifying
// action is gated on. With no profile selected there is nothing to modify, so the default mode
// is what applies.
func (app *App) Mode() config.Mode {
	if app.Selected.Profile == nil {
		return config.Regular
	}
	return app.Config.ProfileMode(app.Selected.Profile.Name)
}

// Modify runs a modifying operation as far as the selected profile's mode allows:
//
//   - read-only refuses it and says so in the status line;
//   - regular puts question in the status line and runs it on <Y>;
//   - yolo runs it straight away.
//
// It must be called on the UI goroutine, which is where a keypress handler runs. run is called
// there too.
func (app *App) Modify(question string, run func()) {
	switch app.Mode() {
	case config.ReadOnly:
		SendStatusWithDefaultTTL(
			"[red]profile " + app.SelectedProfileName() + " is in read-only mode",
		)
	case config.Yolo:
		run()
	default:
		app.Confirm(question, run)
	}
}

// Confirm asks question in the status line and runs onYes once the user answers <Y>. <N> and
// <Esc> abandon the operation; every other key is ignored while the question stands, so nothing
// else in the application reacts until the user decides — see answer.
//
// The answer is taken in either case, whatever the question shows.
func (app *App) Confirm(question string, onYes func()) {
	app.confirm = &confirmation{onYes: onYes}
	SendStatusPrompt(question + " [Y/N]")
}

// answer resolves the standing confirmation and reports whether the keypress was consumed.
//
// It is the gate the application-wide input capture runs first: while a question stands, it
// consumes every keypress, so no page, modal or search field sees any of them.
func (app *App) answer(event *tcell.EventKey) bool {
	pending := app.confirm
	if pending == nil {
		return false
	}

	switch {
	case IsKey(event, 'y'), IsKey(event, 'Y'):
		app.confirm = nil
		// Cleared before onYes runs: the operation puts its own message in the status line.
		ClearStatus()
		pending.onYes()
	case IsKey(event, 'n'), IsKey(event, 'N'), event.Key() == tcell.KeyEsc:
		app.confirm = nil
		SendStatusWithDefaultTTL("cancelled")
	}

	return true
}

// confirmPending reports whether a confirmation is waiting for an answer.
//
// The status line consults it before showing or clearing anything: a standing question outlives
// both a background message and the TTL of the message it displaced, whose timer can have fired
// already by the time the question replaces it.
func (app *App) confirmPending() bool {
	return app.confirm != nil
}
