// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"
)

// Status represents a status message with optional time-to-live
type Status struct {
	Message string
	TTL     time.Duration // 0 means infinite (no auto-clear)
	Spinner bool          // true to show spinner animation
	// Prompt marks a confirmation question. It is the one message shown while a confirmation is
	// pending, since everything else would paint over the question being asked.
	Prompt bool
}

var (
	StatusLineCh    = make(chan Status, 10)
	statusLineTimer *time.Timer
	SpinnerFrames   = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
)

// SendStatus sends a status message with the given TTL and spinner control
func SendStatus(message string, ttl time.Duration, spinner bool) {
	StatusLineCh <- Status{Message: message, TTL: ttl, Spinner: spinner}
}

// SendStatusWithDefaultTTL sends a status message with 10 second TTL and no spinner
func SendStatusWithDefaultTTL(message string) {
	StatusLineCh <- Status{Message: message, TTL: 10 * time.Second, Spinner: false}
}

// SendStatusInfinite sends a status message that never auto-clears with spinner
func SendStatusInfinite(message string) {
	StatusLineCh <- Status{Message: message, TTL: 0, Spinner: true}
}

// SendStatusInfinite sends a status message that never auto-clears without spinner
func SendStatusInfiniteWithouSpinner(message string) {
	StatusLineCh <- Status{Message: message, TTL: 0, Spinner: false}
}

// SendStatusPrompt sends a confirmation question: it never auto-clears, shows no spinner, and
// is the only message displayed until it is answered.
func SendStatusPrompt(message string) {
	StatusLineCh <- Status{Message: message, TTL: 0, Spinner: false, Prompt: true}
}

// ClearStatus clears the status line immediately
func ClearStatus() {
	StatusLineCh <- Status{Message: "", TTL: 0, Spinner: false}
}

// RunStatusLineHandler handles status messages with spinner animation
func (app *App) RunStatusLineHandler(ctx context.Context, in chan Status) {
	go func() {
		spinnerTicker := time.NewTicker(100 * time.Millisecond)
		defer spinnerTicker.Stop()

		var currentStatus string
		var spinnerIdx int
		var spinnerActive bool

		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("shutting down status line handler")
				return
			case status := <-in:
				app.QueueUpdateDraw(func() {
					// A pending confirmation owns the status line: an in-flight fetch reporting
					// progress must not paint over the question the user is being asked.
					if app.confirmPending() && !status.Prompt {
						log.Debug().Str("status", status.Message).
							Msg("status suppressed while a confirmation is pending")
						return
					}

					if status.Message != "" {
						currentStatus = status.Message
						spinnerActive = status.Spinner
						app.Layout.StatusHint.SetText("")

						if status.Spinner {
							app.Layout.StatusLine.SetText(
								fmt.Sprintf(
									"%s %s",
									SpinnerFrames[spinnerIdx],
									status.Message,
								),
							)
						} else {
							app.Layout.StatusLine.SetText(status.Message)
						}

						if statusLineTimer != nil {
							statusLineTimer.Stop()
						}
						if status.TTL > 0 {
							statusLineTimer = time.AfterFunc(status.TTL, func() {
								app.QueueUpdateDraw(func() {
									if app.confirmPending() {
										return
									}
									currentStatus = ""
									spinnerActive = false
									app.Layout.StatusLine.SetText("")
									app.Layout.StatusHint.SetText(
										app.versionHintText(),
									)
									app.resizeStatusHint()
								})
							})
						}
					} else {
						currentStatus = ""
						spinnerActive = false
						app.Layout.StatusLine.SetText("")
						app.Layout.StatusHint.SetText(app.versionHintText())
						app.resizeStatusHint()
						if statusLineTimer != nil {
							statusLineTimer.Stop()
						}
					}
				})
			case <-spinnerTicker.C:
				if spinnerActive {
					spinnerIdx = (spinnerIdx + 1) % len(SpinnerFrames)
					app.QueueUpdateDraw(func() {
						if currentStatus != "" {
							app.Layout.StatusLine.SetText(
								fmt.Sprintf(
									"%s %s",
									SpinnerFrames[spinnerIdx],
									currentStatus,
								),
							)
						}
					})
				}
			}
		}
	}()
}
