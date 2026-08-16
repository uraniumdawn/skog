// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"
	"github.com/sahilm/fuzzy"

	"github.com/uraniumdawn/skog/pkg/util"
)

const (
	// ProfilesResourceEventType is the event type for the AWS profiles resource.
	ProfilesResourceEventType EventType = "resources:profiles"
	// S3ResourceEventType is the event type for the S3 resource.
	S3ResourceEventType EventType = "resources:s3"
)

// resourceEvents maps a Resources modal entry to the event its selection publishes.
var resourceEvents = map[string]EventType{
	Profiles: ProfilesResourceEventType,
	S3:       S3ResourceEventType,
}

// ResourcesChannel is the channel for resource events.
var ResourcesChannel = make(chan Event)

// RunResourcesEventHandler processes resource events from the channel.
func (app *App) RunResourcesEventHandler(ctx context.Context, in chan Event) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("shutting down resource event handler")
				return
			case event := <-in:
				switch event.Type {
				case "prfs", ProfilesResourceEventType:
					Publish(ProfilesChannel, GetProfilesEventType, Payload{nil, false})
				case "s3", S3ResourceEventType:
					if !app.requireSelection(
						app.IsProfileSelected(),
						"[red]to perform operation, select profile",
					) {
						continue
					}
					Publish(S3Channel, GetBucketsEventType, Payload{nil, false})
				default:
					SendStatusWithDefaultTTL("invalid command")
				}
			}
		}
	}()
}

// NewResourcesPage creates the modal listing the resources skog can open.
func (app *App) NewResourcesPage() tview.Primitive {
	table := tview.NewTable()
	table.SetSelectable(true, false).SetBorderPadding(0, 0, 1, 0)

	var allResources []string
	addResource := func(name string) {
		table.SetCell(len(allResources), 0, tview.NewTableCell(name))
		allResources = append(allResources, name)
	}

	addResource(Profiles)
	addResource(S3)

	table.SetSelectedStyle(
		tcell.StyleDefault.Foreground(
			tcell.GetColor(app.Colors.Skog.Selection.FgColor),
		).Background(
			tcell.GetColor(app.Colors.Skog.Selection.BgColor),
		),
	)

	searchInput := tview.NewInputField()
	searchInput.SetLabel(" / ")
	searchInput.SetLabelColor(tcell.GetColor(app.Colors.Skog.Search.FgColor))
	searchInput.SetFieldTextColor(tcell.GetColor(app.Colors.Skog.Search.FgColor))
	searchInput.SetFieldBackgroundColor(tcell.GetColor(app.Colors.Skog.Background))
	searchInput.SetBackgroundColor(tcell.GetColor(app.Colors.Skog.Background))
	app.ResourcesSearchInput = searchInput

	searchInput.SetChangedFunc(func(text string) {
		filterResourcesTable(table, allResources, text)
	})

	searchInput.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		switch event.Key() {
		case tcell.KeyEsc:
			searchInput.SetText("")
			app.SetFocus(table)
			return nil
		case tcell.KeyEnter:
			app.SetFocus(table)
			return nil
		}
		return event
	})

	closeModal := func() {
		searchInput.SetText("")
		app.HideModalPage(Resources)
	}

	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			if table.GetRowCount() == 0 {
				return nil
			}
			row, _ := table.GetSelection()
			resource := table.GetCell(row, 0).Text
			closeModal()
			Publish(ResourcesChannel, resourceEvents[resource], Payload{})
			return nil
		}

		if event.Key() == tcell.KeyEsc {
			closeModal()
			return nil
		}

		if IsKey(event, ':') {
			app.SetFocus(searchInput)
			return nil
		}

		return event
	})

	container := tview.NewFlex().SetDirection(tview.FlexRow)
	container.SetBorder(true).
		SetTitle(" Resources ")
	container.AddItem(searchInput, 1, 0, false)
	container.AddItem(table, 0, 1, true)

	// +2 for border, +1 for search row
	height := len(allResources) + 3
	return util.NewResourceModal(container, height)
}

// filterResourcesTable filters the resources table using fuzzy matching.
// An empty filter restores all resources in their original order.
func filterResourcesTable(table *tview.Table, allResources []string, filter string) {
	table.Clear()
	if filter == "" {
		for i, name := range allResources {
			table.SetCell(i, 0, tview.NewTableCell(name))
		}
	} else {
		matches := fuzzy.Find(filter, allResources)
		for i, match := range matches {
			table.SetCell(i, 0, tview.NewTableCell(match.Str))
		}
	}
	if table.GetRowCount() > 0 {
		table.Select(0, 0)
	}
}
