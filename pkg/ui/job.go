// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import "context"

// A job is a walk over the keys of a prefix: aggregating them (<i>) or downloading them (<d>).
// Unlike the single call a page is built from, it takes as many requests as the prefix has keys
// and runs under no timeout.
//
// Only one job runs at a time. Letting them pile up would spend the user's requests on rows they
// are no longer looking at, and <Esc> — the one key that ends a job — has to be unambiguous about
// which one it ends.

// beginJob claims the job slot for a job named what ("a scan", "a download"), reporting whether
// it was free. A refused job names the one holding the slot, since that is what <Esc> stops.
func (app *App) beginJob(what string) bool {
	app.jobMu.Lock()
	running := app.jobName
	if running == "" {
		app.jobName = what
	}
	app.jobMu.Unlock()

	if running != "" {
		SendStatusWithDefaultTTL(
			"[red]" + running + " is already running, press <Esc> to cancel it",
		)
		return false
	}
	return true
}

// tryBeginJob claims the slot for a job nobody asked for — one a page starts on its own — and
// reports whether it was free.
//
// Unlike beginJob it says nothing when it is not: a user who did not ask for this job has no
// reason to be told it could not run, and what it would have filled in is filled in on demand
// instead.
func (app *App) tryBeginJob(what string) bool {
	app.jobMu.Lock()
	defer app.jobMu.Unlock()

	if app.jobName != "" {
		return false
	}
	app.jobName = what
	return true
}

// endJob releases the job slot.
func (app *App) endJob() {
	app.jobMu.Lock()
	defer app.jobMu.Unlock()

	app.jobName = ""
	app.jobCancel = nil
}

// setJobCancel records how to cancel the running job.
func (app *App) setJobCancel(cancel context.CancelFunc) {
	app.jobMu.Lock()
	defer app.jobMu.Unlock()

	app.jobCancel = cancel
}

// CancelJob stops the running job, if any, and reports whether there was one.
func (app *App) CancelJob() bool {
	app.jobMu.Lock()
	cancel := app.jobCancel
	app.jobCancel = nil
	app.jobMu.Unlock()

	if cancel == nil {
		return false
	}
	cancel()
	return true
}
