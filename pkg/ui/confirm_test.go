// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/uraniumdawn/skog/pkg/awscfg"
	"github.com/uraniumdawn/skog/pkg/config"
)

// newConfirmApp builds the least App a confirmation needs: a selected profile in the given mode,
// and no UI.
func newConfirmApp(mode config.Mode) *App {
	app := &App{
		Config:   &config.Config{},
		Selected: Selected{Profile: &awscfg.Profile{Name: "test"}},
	}
	app.Config.SetProfileMode("test", mode)
	return app
}

// drainStatus empties the status channel, which has no handler running in a test.
func drainStatus() {
	for {
		select {
		case <-StatusLineCh:
		default:
			return
		}
	}
}

func keyRune(r rune) *tcell.EventKey {
	return tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
}

func TestConfirmAnswers(t *testing.T) {
	tests := []struct {
		name    string
		event   *tcell.EventKey
		wantYes bool
		// wantPending is whether the question still stands after the keypress.
		wantPending bool
	}{
		{name: "y confirms", event: keyRune('y'), wantYes: true},
		{name: "uppercase Y confirms", event: keyRune('Y'), wantYes: true},
		{name: "n abandons", event: keyRune('n')},
		{name: "uppercase N abandons", event: keyRune('N')},
		{
			name:  "esc abandons",
			event: tcell.NewEventKey(tcell.KeyEsc, 0, tcell.ModNone),
		},
		{name: "any other rune is ignored", event: keyRune('j'), wantPending: true},
		{
			name:        "enter is ignored",
			event:       tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone),
			wantPending: true,
		},
		{
			name:        "x is ignored, so the question cannot re-ask itself",
			event:       keyRune('x'),
			wantPending: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer drainStatus()

			app := newConfirmApp(config.Regular)
			yes := 0
			app.Confirm("delete it?", func() { yes++ })

			if !app.confirmPending() {
				t.Fatalf("no question pending after Confirm()")
			}

			// Every keypress is consumed while a question stands: that is what blocks the
			// application, whether or not the key means anything.
			if !app.answer(tt.event) {
				t.Errorf("answer() = false, want the keypress consumed")
			}
			if got := yes > 0; got != tt.wantYes {
				t.Errorf("onYes ran = %v, want %v", got, tt.wantYes)
			}
			if got := app.confirmPending(); got != tt.wantPending {
				t.Errorf("question still pending = %v, want %v", got, tt.wantPending)
			}
		})
	}
}

// A question is answered once: the operation must not run twice on a second <y>.
func TestConfirmRunsOnceOnRepeatedYes(t *testing.T) {
	defer drainStatus()

	app := newConfirmApp(config.Regular)
	yes := 0
	app.Confirm("delete it?", func() { yes++ })

	app.answer(keyRune('y'))
	if consumed := app.answer(keyRune('y')); consumed {
		t.Errorf("answer() = true with no question pending, want the keypress passed through")
	}
	if yes != 1 {
		t.Errorf("onYes ran %d times, want 1", yes)
	}
}

// What a mode does to a modifying operation is the whole point of having modes.
func TestModifyByMode(t *testing.T) {
	tests := []struct {
		mode config.Mode
		// wantRan is whether the operation ran without the user answering anything.
		wantRan     bool
		wantPending bool
	}{
		{mode: config.ReadOnly},
		{mode: config.Regular, wantPending: true},
		{mode: config.Yolo, wantRan: true},
	}

	for _, tt := range tests {
		t.Run(string(tt.mode), func(t *testing.T) {
			defer drainStatus()

			app := newConfirmApp(tt.mode)
			ran := 0
			app.Modify("delete it?", func() { ran++ })

			if got := ran > 0; got != tt.wantRan {
				t.Errorf("operation ran = %v, want %v", got, tt.wantRan)
			}
			if got := app.confirmPending(); got != tt.wantPending {
				t.Errorf("question pending = %v, want %v", got, tt.wantPending)
			}

			// Only a standing question swallows keys: refused and already run both leave the
			// application answering to its own keys again.
			if got := app.answer(keyRune('y')); got != tt.wantPending {
				t.Errorf("answer() consumed the keypress = %v, want %v", got, tt.wantPending)
			}
			if tt.mode == config.ReadOnly && ran != 0 {
				t.Errorf("operation ran %d times in read-only, want 0", ran)
			}
		})
	}
}

// A profile is regular until it is switched, and with no profile selected there is nothing to
// modify — neither may end up running an operation unasked.
func TestModeDefaults(t *testing.T) {
	app := newConfirmApp(config.Regular)
	if got := app.Mode(); got != config.Regular {
		t.Errorf("Mode() = %q, want %q", got, config.Regular)
	}

	app.Selected.Profile = nil
	if got := app.Mode(); got != config.Regular {
		t.Errorf("Mode() with no profile = %q, want %q", got, config.Regular)
	}
}

func TestAnswerPassesKeysThroughWithNoQuestion(t *testing.T) {
	app := newConfirmApp(config.Regular)
	if app.answer(keyRune('y')) {
		t.Errorf("answer() = true with no question pending, want false")
	}
}

// The question is the one message the status line shows while it stands; a background fetch's
// progress must not paint over it.
func TestConfirmSendsAPromptStatus(t *testing.T) {
	defer drainStatus()

	app := newConfirmApp(config.Regular)
	app.Confirm("Delete the selected object?", func() {})

	select {
	case status := <-StatusLineCh:
		if !status.Prompt {
			t.Errorf("status.Prompt = false, want the question marked as a prompt")
		}
		if status.TTL != 0 {
			t.Errorf("status.TTL = %v, want no auto-clear", status.TTL)
		}
		if status.Spinner {
			t.Errorf("status.Spinner = true, want no spinner on a question")
		}
		if want := "Delete the selected object? [Y/N]"; status.Message != want {
			t.Errorf("status.Message = %q, want %q", status.Message, want)
		}
	default:
		t.Fatalf("Confirm() sent no status message")
	}
}
