// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"context"
	"fmt"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"

	"github.com/uraniumdawn/skog/pkg/awscfg"
)

// GetProfilesEventType opens the AWS profiles page.
const GetProfilesEventType EventType = "profiles:get"

// ProfilesChannel is the channel for profile events.
var ProfilesChannel = make(chan Event)

// activeMarker marks the selected profile in the profiles list.
const activeMarker = "✓"

// activeColumn is the index of the "Active" column in the profiles table.
const activeColumn = 5

// RunProfilesEventHandler processes profile events from the channel.
func (app *App) RunProfilesEventHandler(ctx context.Context, in chan Event) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Debug().Msg("shutting down profiles event handler")
				return
			case event := <-in:
				if event.Type != GetProfilesEventType {
					continue
				}
				app.QueueUpdateDraw(func() {
					// Profiles come from local files, so opening the page is a re-read: cheap
					// enough to do every time, and what picks up a profile added to ~/.aws
					// while skog was running. There is nothing here to refresh by hand.
					if err := app.ReloadProfiles(); err != nil {
						log.Error().Err(err).Msg("failed to reload aws configuration")
						SendStatusWithDefaultTTL(
							fmt.Sprintf("[red]failed to read aws config: %s", err.Error()),
						)
					}
					table := app.NewProfilesTable()
					app.ProfilesTableInputHandler(table)
					app.AddToPagesRegistry(Profiles, table, ProfilesPageMenu, false)
					if len(app.Profiles) == 0 {
						SendStatusWithDefaultTTL(
							"[red]no profiles found in " + awscfg.ConfigPath() +
								" or " + awscfg.CredentialsPath(),
						)
					}
				})
			}
		}
	}()
}

// NewProfilesTable builds the table of AWS profiles discovered in ~/.aws.
func (app *App) NewProfilesTable() *tview.Table {
	table := tview.NewTable()
	table.SetTitle(" Profiles ")
	table.SetSelectable(true, false).
		SetBorder(true).
		SetBorderPadding(0, 0, 1, 0)
	table.SetSelectedStyle(
		tcell.StyleDefault.Foreground(
			tcell.GetColor(app.Colors.Skog.Selection.FgColor),
		).Background(
			tcell.GetColor(app.Colors.Skog.Selection.BgColor),
		),
	)
	table.SetFixed(1, 0)

	labelColor := tcell.GetColor(app.Colors.Skog.Label.FgColor)
	setProfilesTableHeader(table, labelColor)

	for row, profile := range app.Profiles {
		app.setProfileRow(table, row+1, profile)
	}
	return table
}

func setProfilesTableHeader(table *tview.Table, labelColor tcell.Color) {
	headers := []string{"Name", "Region", "Endpoint", "Credentials", "Source", "Active"}
	for col, text := range headers {
		table.SetCell(0, col, tview.NewTableCell(text).
			SetSelectable(false).
			SetTextColor(labelColor))
	}
}

func (app *App) setProfileRow(table *tview.Table, row int, profile *awscfg.Profile) {
	endpoint := profile.EndpointURL
	if endpoint == "" {
		endpoint = "aws"
	}
	credentials := "-"
	if profile.HasCredentials {
		credentials = "static keys"
	}

	table.
		SetCell(row, 0, tview.NewTableCell(profile.Name)).
		SetCell(row, 1, tview.NewTableCell(profile.Region)).
		SetCell(row, 2, tview.NewTableCell(endpoint)).
		SetCell(row, 3, tview.NewTableCell(credentials)).
		SetCell(row, 4, tview.NewTableCell(profileSource(profile))).
		SetCell(row, activeColumn, tview.NewTableCell(app.activeMark(profile)))
}

// activeMark returns the marker shown for the selected profile.
func (app *App) activeMark(profile *awscfg.Profile) string {
	if app.Selected.Profile != nil && app.Selected.Profile.Name == profile.Name {
		return activeMarker
	}
	return ""
}

// profileSource names the files that declared the profile, which is what explains a profile
// that lists no credentials of its own.
func profileSource(profile *awscfg.Profile) string {
	switch {
	case profile.InConfig && profile.InCredentials:
		return "config, credentials"
	case profile.InCredentials:
		return "credentials"
	default:
		return "config"
	}
}

// ProfilesTableInputHandler wires Enter: selecting the profile to work with.
func (app *App) ProfilesTableInputHandler(table *tview.Table) {
	table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			row, _ := table.GetSelection()
			if row < 1 || row > len(app.Profiles) {
				return nil
			}
			name := table.GetCell(row, 0).Text
			profile := app.ProfileByName(name)
			if profile == nil {
				return nil
			}

			app.SelectProfile(profile, true)
			for i, p := range app.Profiles {
				table.GetCell(i+1, activeColumn).SetText(app.activeMark(p))
			}
			SendStatusWithDefaultTTL("profile " + profile.Name + " selected")
			return nil
		}

		return event
	})
}
