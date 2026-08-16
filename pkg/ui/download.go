// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"errors"
	"fmt"

	"github.com/uraniumdawn/skog/pkg/s3"
	"github.com/uraniumdawn/skog/pkg/util"
)

// Download writes what a row points at into the download folder (skog.download.dir): one object,
// or every key under a folder, keeping the hierarchy as <dir>/<bucket>/<key>.
//
// A download reads S3 and writes the local disk, so it is not gated on the profile's mode — a
// read-only profile can still be downloaded from. A folder is asked about first: S3 says nothing
// about what a prefix holds until it is walked, so the row gives no hint of how much is coming.
//
// It must be called on the UI goroutine, which is where a keypress handler runs.
func (app *App) Download(bucket, key string, folder bool) {
	dir, err := app.Config.DownloadDir()
	if err != nil {
		SendStatusWithDefaultTTL(fmt.Sprintf("[red]invalid download.dir: %s", err.Error()))
		return
	}

	if folder {
		// The question names no prefix: one can be long enough to push the answer off the status
		// line, and the row it acts on is the highlighted one anyway.
		app.Confirm("Download everything under the selected folder?", func() {
			app.download(bucket, key, dir, true)
		})
		return
	}
	app.download(bucket, key, dir, false)
}

// download fetches one object, or a whole prefix, into dir. It holds the job slot for as long as
// it runs, so a download and a scan never compete for the user's requests; see job.go.
func (app *App) download(bucket, key, dir string, folder bool) {
	path := s3.DisplayPath(bucket, key)

	if !app.beginJob("a download") {
		return
	}

	// A prefix takes one request per object, and a single object can be gigabytes: the
	// single-call timeout does not apply. A download ends when it is done, when the user cancels
	// it, or when the application exits.
	ctx, cancel := context.WithCancel(app.ctx)
	app.setJobCancel(cancel)

	SendStatusInfinite("downloading " + path + " (<Esc> to cancel)")

	go func() {
		defer cancel()
		defer app.endJob()

		client, err := app.S3Client(ctx)
		if err != nil {
			failed("downloading "+path, err)
			return
		}

		var result *s3.Download
		if folder {
			result, err = client.DownloadPrefix(ctx, bucket, key, dir, downloadProgress(path))
		} else {
			result, err = client.DownloadObject(ctx, bucket, key, dir)
		}

		switch {
		// A cancelled read of a body surfaces as whatever the transport made of it, so the
		// context is what says the user is the one who ended the download.
		case err != nil && (errors.Is(err, context.Canceled) || ctx.Err() != nil):
			SendStatusWithDefaultTTL("download of " + path + " cancelled")
		case err != nil:
			failed("downloading "+path, err)
		default:
			SendStatusWithDefaultTTL(downloadSummary(path, result))
		}
	}()
}

// downloadProgressObjects is how many objects pass between progress reports. Reporting every
// object would redraw the status line once per key for no added information.
const downloadProgressObjects = 10

// downloadProgress reports how far a recursive download has got, every downloadProgressObjects
// objects.
func downloadProgress(label string) func(int, string) {
	return func(done int, _ string) {
		if done%downloadProgressObjects != 0 {
			return
		}
		SendStatusInfinite(
			fmt.Sprintf(
				"downloading %s — %s objects (<Esc> to cancel)",
				label,
				util.FormatNumber(int64(done)),
			),
		)
	}
}

// downloadSummary reports what a finished download wrote, and where.
func downloadSummary(path string, result *s3.Download) string {
	if result.Objects == 1 && !result.Partial {
		return fmt.Sprintf(
			"downloaded %s (%s) → %s",
			path,
			util.FormatBytes(result.Bytes),
			result.Path,
		)
	}

	summary := fmt.Sprintf(
		"downloaded %s: %s objects, %s → %s",
		path,
		util.FormatNumber(int64(result.Objects)),
		util.FormatBytes(result.Bytes),
		result.Path,
	)
	if result.Partial {
		summary += " (partial: scanned-keys cap reached)"
	}
	return summary
}
