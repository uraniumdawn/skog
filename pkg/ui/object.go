// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"

	"github.com/gdamore/tcell/v2"

	"github.com/uraniumdawn/skog/pkg/s3"
)

// Object fetches an object's metadata and opens it as a description page. Only the metadata
// is read; the object body is never fetched.
func (app *App) Object(target ObjectTarget) {
	pageKey := app.objectPageKey(target)
	path := s3.DisplayPath(target.Bucket, target.Key)

	fetch(app, "getting metadata of "+path,
		func(ctx context.Context, client *s3.Client) (*s3.ObjectInfo, error) {
			return client.HeadObject(ctx, target.Bucket, target.Key)
		},
		func(info *s3.ObjectInfo) {
			desc := app.NewDescription(" " + path + " ")
			desc.SetText(info.String())
			desc.SetInputCapture(
				app.WithHScroll(desc, func(event *tcell.EventKey) *tcell.EventKey {
					if event.Key() == tcell.KeyCtrlU {
						Publish(S3Channel, GetObjectEventType, Payload{target, true})
						return nil
					}
					return event
				}),
			)
			app.AddToPagesRegistry(pageKey, desc, ObjectDescriptionPageMenu, false)
			// Above an object is the level its key sits at, below it what the object holds.
			// An object always sits somewhere, so the parent is never in doubt here.
			parent, _ := s3.Parent(target.Key)
			app.Layout.PagesRegistry.SetPageNavigation(pageKey,
				func() {
					Publish(
						S3Channel,
						GetObjectsEventType,
						Payload{ObjectsTarget{Bucket: target.Bucket, Prefix: parent}, false},
					)
				},
				func() {
					app.ViewData(ObjectDataTarget{Bucket: target.Bucket, Key: target.Key})
				},
			)
		},
	)
}
