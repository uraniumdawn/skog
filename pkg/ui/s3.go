// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/uraniumdawn/skog/pkg/s3"
)

const (
	// GetBucketsEventType opens the bucket list of the selected profile.
	GetBucketsEventType EventType = "s3:buckets"
	// GetObjectsEventType opens one level of a bucket's key hierarchy.
	GetObjectsEventType EventType = "s3:objects"
	// GetObjectEventType opens a single object's metadata.
	GetObjectEventType EventType = "s3:object"
	// GetObjectDataEventType opens what a single object holds, as a table.
	GetObjectDataEventType EventType = "s3:object-data"
)

// S3Channel carries every S3 navigation event: buckets, a prefix level, an object.
var S3Channel = make(chan Event)

// ObjectsTarget identifies one level of a bucket's key hierarchy. Prefix is empty for the
// root of the bucket and otherwise ends with a delimiter.
type ObjectsTarget struct {
	Bucket string
	Prefix string
}

// ObjectTarget identifies a single object.
type ObjectTarget struct {
	Bucket string
	Key    string
}

// ObjectDataTarget identifies the object a viewer page shows. It is its own type so that the
// metadata page and the viewer page of the same object are distinct events.
type ObjectDataTarget struct {
	Bucket string
	Key    string
}

// RunS3EventHandler processes S3 navigation events from the channel.
//
// Every event resolves to a page: an already opened page is switched to, unless the event
// forces a refresh, in which case it is fetched again.
func (app *App) RunS3EventHandler(ctx context.Context, in chan Event) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("shutting down s3 event handler")
				return
			case event := <-in:
				switch event.Type {
				case GetBucketsEventType:
					app.openPage(app.bucketsPageKey(), event.Payload.Force, app.Buckets)

				case GetObjectsEventType:
					target, ok := event.Payload.Data.(ObjectsTarget)
					if !ok {
						log.Error().Msg("objects event without a target")
						continue
					}
					app.openPage(app.objectsPageKey(target), event.Payload.Force, func() {
						app.Objects(target)
					})

				case GetObjectEventType:
					target, ok := event.Payload.Data.(ObjectTarget)
					if !ok {
						log.Error().Msg("object event without a target")
						continue
					}
					app.openPage(app.objectPageKey(target), event.Payload.Force, func() {
						app.Object(target)
					})

				case GetObjectDataEventType:
					target, ok := event.Payload.Data.(ObjectDataTarget)
					if !ok {
						log.Error().Msg("object data event without a target")
						continue
					}
					app.openPage(app.objectDataPageKey(target), event.Payload.Force, func() {
						app.ObjectData(target)
					})
				}
			}
		}
	}()
}

// openPage switches to an already opened page, or calls fetch to build it. A forced event
// always fetches.
func (app *App) openPage(pageKey string, force bool, fetch func()) {
	if _, found := app.Cache.Get(pageKey); found && !force {
		app.QueueUpdateDraw(func() {
			app.SwitchToPage(pageKey)
		})
		return
	}
	fetch()
}

// bucketsPageKey is the page key of the selected profile's bucket list.
func (app *App) bucketsPageKey() string {
	return app.SelectedProfileName() + ":s3:buckets"
}

// objectsPageKey is the page key of one level of a bucket's hierarchy. S3 keys are
// case-sensitive, so the key is built verbatim rather than lower-cased.
func (app *App) objectsPageKey(target ObjectsTarget) string {
	return app.SelectedProfileName() + ":s3:" + s3.DisplayPath(target.Bucket, target.Prefix)
}

// objectPageKey is the page key of an object's metadata page. The ":info" suffix keeps it
// distinct from the listing of a prefix with the same name, which a folder marker object
// would otherwise collide with.
func (app *App) objectPageKey(target ObjectTarget) string {
	return app.SelectedProfileName() + ":s3:" + s3.DisplayPath(target.Bucket, target.Key) + ":info"
}

// objectDataPageKey is the page key of an object's viewer page. The ":data" suffix keeps it
// distinct from the object's metadata page and from a prefix of the same name.
func (app *App) objectDataPageKey(target ObjectDataTarget) string {
	return app.SelectedProfileName() + ":s3:" + s3.DisplayPath(target.Bucket, target.Key) + ":data"
}

// objectSchemaPageKey is the page key of the schema of the object being viewed, kept apart
// from its rows the way the metadata page is kept apart from both.
func (app *App) objectSchemaPageKey(target ObjectDataTarget) string {
	return app.SelectedProfileName() + ":s3:" + s3.DisplayPath(target.Bucket, target.Key) + ":schema"
}

// fetch runs an S3 call off the UI goroutine and hands its result to render on the UI
// goroutine. what names the operation in the status line and in error messages.
func fetch[T any](
	app *App,
	what string,
	call func(context.Context, *s3.Client) (T, error),
	render func(T),
) {
	SendStatusInfinite(what)

	withClient(app, what, func(ctx context.Context, client *s3.Client) error {
		result, err := call(ctx, client)
		if err != nil {
			return err
		}

		app.QueueUpdateDraw(func() {
			render(result)
			ClearStatus()
		})
		return nil
	})
}

// perform runs an S3 call that changes something off the UI goroutine, applies apply on the UI
// goroutine and leaves done in the status line. Unlike fetch it does not clear the status line:
// a change has no result to render, so what happened is what is worth reporting.
func perform(
	app *App,
	what, done string,
	call func(context.Context, *s3.Client) error,
	apply func(),
) {
	SendStatusInfinite(what)

	withClient(app, what, func(ctx context.Context, client *s3.Client) error {
		if err := call(ctx, client); err != nil {
			return err
		}

		app.QueueUpdateDraw(apply)
		SendStatusWithDefaultTTL(done)
		return nil
	})
}

// withClient runs fn off the UI goroutine against the selected profile's client, under the
// configured API call timeout, and reports a failure in the status line.
func withClient(app *App, what string, fn func(context.Context, *s3.Client) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), app.Config.GetAPICallTimeout())
		defer cancel()

		client, err := app.S3Client(ctx)
		if err != nil {
			failed(what, err)
			return
		}

		if err := fn(ctx, client); err != nil {
			failed(what, err)
		}
	}()
}

// failed logs an S3 failure and reports it in the status line.
func failed(what string, err error) {
	log.Error().Err(err).Str("operation", what).Msg("s3 call failed")
	SendStatusWithDefaultTTL(fmt.Sprintf("[red]failed %s: %s", what, err.Error()))
}
