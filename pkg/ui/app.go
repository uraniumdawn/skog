// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

// Package ui provides the terminal user interface for the skog application.
package ui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/patrickmn/go-cache"
	"github.com/rivo/tview"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/uraniumdawn/skog/pkg/awscfg"
	"github.com/uraniumdawn/skog/pkg/config"
	"github.com/uraniumdawn/skog/pkg/s3"
)

// Version is the application version injected at startup via main.
var Version = ""

// Names of the pages that exist for the whole session. They double as page keys and, for
// Profiles and S3, as the entries of the Resources modal. Pages opened per bucket or prefix
// build their keys from the profile, bucket and key instead (see bucketsPageKey).
const (
	Resources   = "Resources"
	Profiles    = "Profiles"
	S3          = "S3"
	OpenedPages = "Opened pages"
)

// ErrNoProfile is returned when an operation needs an AWS profile and none is selected.
var ErrNoProfile = errors.New("no profile selected")

type App struct {
	*tview.Application
	Layout *Layout
	Cache  *cache.Cache

	// Profiles are the AWS profiles discovered in ~/.aws, in the order the files declare
	// them. They are read-only: skog never writes to the AWS configuration.
	Profiles []*awscfg.Profile

	// S3Clients holds one client per profile name, built on first use and kept for the
	// session. clientsMu guards it, since clients are created from fetch goroutines.
	S3Clients map[string]*s3.Client
	clientsMu sync.Mutex

	// ctx is cancelled when the application exits; background work derives from it.
	ctx context.Context

	// scanning and scanCancel track the one running prefix aggregate, guarded by scanMu.
	scanning   bool
	scanCancel context.CancelFunc
	scanMu     sync.Mutex

	Selected             Selected
	Config               *config.Config
	Colors               *config.ColorConfig
	CurrentFilters       map[string]string // pageName -> filter text for search preservation
	ResourcesSearchInput *tview.InputField
}

// Selected holds what the user is working with. Every S3 operation runs against it.
type Selected struct {
	Profile *awscfg.Profile
}

func NewApp() *App {
	// Save real stderr before InitLogger redirects os.Stderr to the log file.
	stderr := os.Stderr
	InitLogger()

	fatal := func(msg string, err error) {
		_, _ = fmt.Fprintf(stderr, "skog: %s: %v\n", msg, err)
		os.Exit(1)
	}

	cfg, err := config.LoadAppConfig()
	if err != nil {
		fatal("failed to load config", err)
	}

	colors, err := config.LoadColorConfig(cfg.Skog.Style)
	if err != nil {
		fatal("failed to load style", err)
	}

	// A broken ~/.aws is worth reporting, but not worth refusing to start over: the user can
	// fix the file and reload the Profiles page with Ctrl+U.
	profiles, err := awscfg.LoadProfiles()
	if err != nil {
		log.Error().Err(err).Msg("failed to read aws configuration")
	}

	return &App{
		Application: tview.NewApplication(),
		// Pages are cached for the whole session: an opened page stays as it was until it
		// is refreshed with Ctrl+U or removed from the opened pages.
		Cache:          cache.New(cache.NoExpiration, cache.NoExpiration),
		Profiles:       profiles,
		S3Clients:      make(map[string]*s3.Client),
		Config:         cfg,
		Colors:         colors,
		CurrentFilters: make(map[string]string),
	}
}

func InitLogger() {
	zerolog.TimeFieldFormat = time.RFC3339

	logFilePath := filepath.Join(os.Getenv("HOME"), ".config", "skog", "skog.log")
	logDir := filepath.Dir(logFilePath)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		fmt.Printf("failed to create log directory, %s\n", err.Error())
	}

	file, err := os.OpenFile(logFilePath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		fmt.Printf("failed to open log file, %s\n", err.Error())
		os.Exit(1)
	}

	// Use ConsoleWriter for human-readable, canonical log format
	consoleWriter := zerolog.ConsoleWriter{
		Out:        file,
		TimeFormat: time.RFC3339,
		NoColor:    true,
	}

	os.Stderr = file
	os.Stdout = file

	// Add caller information (file and line number) to all log entries
	log.Logger = log.Output(consoleWriter).With().Caller().Logger()
}

func (app *App) Run() {
	app.ApplyColors()
	ctx, cancel := context.WithCancel(context.Background())
	app.ctx = ctx

	app.RunResourcesEventHandler(ctx, ResourcesChannel)
	app.RunStatusLineHandler(ctx, StatusLineCh)
	app.RunProfilesEventHandler(ctx, ProfilesChannel)
	app.RunS3EventHandler(ctx, S3Channel)

	registry := NewPagesRegistry(app.Colors)
	app.Layout = NewLayout(registry, app.Colors)

	if name := app.Config.SelectedProfile(); name != "" {
		if profile := app.ProfileByName(name); profile != nil {
			app.SelectProfile(profile, false)
		} else {
			log.Warn().Str("profile", name).Msg("remembered profile is gone from ~/.aws")
		}
	}

	Publish(ProfilesChannel, GetProfilesEventType, Payload{nil, false})
	app.Layout.SetSelected(app.Selected.Profile)

	registry.UI.Pages.AddPage(Resources, app.NewResourcesPage(), true, false)
	registry.UI.Pages.AddPage(OpenedPages, registry.UI.Main, true, false)
	registry.UI.Pages.ShowPage(Profiles)
	app.Layout.Menu.SetMenu(ProfilesPageMenu)
	registry.UI.FilteredPages.SetSelectedStyle(
		tcell.StyleDefault.Foreground(
			tcell.GetColor(app.Colors.Skog.Selection.FgColor),
		).Background(
			tcell.GetColor(app.Colors.Skog.Selection.BgColor),
		),
	)

	app.OpenPagesKeyHandler(registry.UI.FilteredPages)
	app.MainOperationKeyHandler()

	if err := app.SetRoot(app.Layout.Content, true).Run(); err != nil {
		log.Error().Err(err).Msg("failed application execution")
	}
	cancel()

	log.Info().Msg("application terminated")
}

func (app *App) ApplyColors() {
	tview.Styles = tview.Theme{
		PrimitiveBackgroundColor:    tcell.GetColor(app.Colors.Skog.Background),
		ContrastBackgroundColor:     tview.Styles.ContrastBackgroundColor,
		MoreContrastBackgroundColor: tview.Styles.MoreContrastBackgroundColor,
		BorderColor:                 tcell.GetColor(app.Colors.Skog.Border),
		TitleColor:                  tcell.GetColor(app.Colors.Skog.Title),
		GraphicsColor:               tview.Styles.GraphicsColor,
		PrimaryTextColor:            tcell.GetColor(app.Colors.Skog.Foreground),
		SecondaryTextColor:          tview.Styles.SecondaryTextColor,
		TertiaryTextColor:           tview.Styles.TertiaryTextColor,
		InverseTextColor:            tview.Styles.InverseTextColor,
		ContrastSecondaryTextColor:  tview.Styles.ContrastSecondaryTextColor,
	}
}

// ProfileByName returns the discovered profile with the given name, or nil.
func (app *App) ProfileByName(name string) *awscfg.Profile {
	for _, profile := range app.Profiles {
		if profile.Name == name {
			return profile
		}
	}
	return nil
}

// ReloadProfiles re-reads ~/.aws and keeps the current selection if that profile still
// exists. It reports whether the files could be read.
func (app *App) ReloadProfiles() error {
	profiles, err := awscfg.LoadProfiles()
	if err != nil {
		return err
	}

	app.Profiles = profiles
	if app.Selected.Profile != nil {
		app.Selected.Profile = app.ProfileByName(app.Selected.Profile.Name)
		app.Layout.SetSelected(app.Selected.Profile)
	}
	return nil
}

// IsProfileSelected reports whether a profile is selected.
func (app *App) IsProfileSelected() bool {
	return app.Selected.Profile != nil
}

// requireSelection sends msg as a status message and returns false if selected is false.
// Callers use the return value to decide whether to abort the current operation.
func (app *App) requireSelection(selected bool, msg string) bool {
	if !selected {
		SendStatusWithDefaultTTL(msg)
		return false
	}
	return true
}

// SelectProfile makes profile the one every S3 operation runs against. The client itself is
// built on first use, off the UI goroutine, since resolving credentials may touch disk or the
// network.
func (app *App) SelectProfile(profile *awscfg.Profile, save bool) {
	app.Selected.Profile = profile
	if app.Layout != nil {
		app.Layout.SetSelected(profile)
	}

	if !save {
		return
	}
	app.Config.Skog.Profile = profile.Name
	if err := app.Config.Save(); err != nil {
		log.Error().Err(err).Msg("failed to save config after profile selection")
		SendStatusWithDefaultTTL(fmt.Sprintf("[red]failed to save config: %s", err.Error()))
	}
}

// S3Client returns the client for the selected profile, building it on first use.
//
// It must be called off the UI goroutine: resolving a profile's credentials can read the SSO
// cache or call STS.
func (app *App) S3Client(ctx context.Context) (*s3.Client, error) {
	app.clientsMu.Lock()
	defer app.clientsMu.Unlock()

	profile := app.Selected.Profile
	if profile == nil {
		return nil, ErrNoProfile
	}
	if client, ok := app.S3Clients[profile.Name]; ok {
		return client, nil
	}

	client, err := s3.NewClient(ctx, profile, s3.Settings{
		MaxRequestedEntries: app.Config.GetMaxRequestedEntries(),
		MaxScannedKeys:      app.Config.GetMaxScannedKeys(),
	})
	if err != nil {
		return nil, err
	}
	app.S3Clients[profile.Name] = client
	return client, nil
}

// SelectedProfileName returns the selected profile's name, or "" when none is selected.
func (app *App) SelectedProfileName() string {
	if app.Selected.Profile == nil {
		return ""
	}
	return app.Selected.Profile.Name
}

func (app *App) NewDescription(title string) *tview.TextView {
	desc := tview.NewTextView().
		SetTextAlign(tview.AlignLeft).
		SetDynamicColors(true).
		SetWrap(false)
	desc.
		SetBorder(true).
		SetBorderPadding(0, 0, 1, 0).
		SetTitle(title)
	desc.SetTextColor(tcell.GetColor(app.Colors.Skog.Foreground))
	return desc
}

// WithHScroll wraps an input capture handler to add H/L horizontal scrolling to a TextView.
func (app *App) WithHScroll(
	desc *tview.TextView,
	handler func(*tcell.EventKey) *tcell.EventKey,
) func(*tcell.EventKey) *tcell.EventKey {
	return func(event *tcell.EventKey) *tcell.EventKey {
		row, col := desc.GetScrollOffset()
		if IsKey(event, 'H') {
			if col > 0 {
				desc.ScrollTo(row, col-5)
			}
			return nil
		}
		if IsKey(event, 'L') {
			desc.ScrollTo(row, col+5)
			return nil
		}
		return handler(event)
	}
}

// versionHintText returns the StatusHint display text.
func (app *App) versionHintText() string {
	return hintText()
}

// resizeStatusHint updates the StatusBar item width to fit the current hint text.
func (app *App) resizeStatusHint() {
	app.Layout.StatusBar.ResizeItem(app.Layout.StatusHint, len([]rune(hintText())), 0)
}

// ClearCurrentFilter clears the saved filter for the current page.
func (app *App) ClearCurrentFilter() {
	currentPage, _ := app.Layout.PagesRegistry.UI.Pages.GetFrontPage()
	delete(app.CurrentFilters, currentPage)

	if search, exists := app.Layout.Search[currentPage]; exists {
		search.SetText("")
	}
}

// ClearFilterForPage clears the saved filter for a specific page.
func (app *App) ClearFilterForPage(pageName string) {
	delete(app.CurrentFilters, pageName)
}
