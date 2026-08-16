// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"io"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"github.com/rivo/tview"
	"github.com/rs/zerolog/log"

	"github.com/uraniumdawn/skog/pkg/config"
)

// PagesRegistry manages the application's pages and page-menu mappings.
type PagesRegistry struct {
	UI              *UI
	PageMenuMap     map[string]string
	SearchablePages []string
	// Closers holds what a page has open, keyed by page name. A page is cached for the whole
	// session, so a viewer page holding a file open holds it until that page is built again
	// over the same key, or until the application exits.
	Closers map[string]io.Closer
	// Ascend and Descend hold how a page moves one level up and one level down the hierarchy
	// it shows, keyed by page name. They are what <h> and <l> do. What is above a level is a
	// property of the level itself rather than of the order pages happen to have been opened
	// in, so each page registers its own.
	Ascend  map[string]func()
	Descend map[string]func()
}

// UI holds the pages every screen of the application is one of.
type UI struct {
	Pages *tview.Pages
}

// Expiration is how long a page stays in the cache. Pages never expire: an opened page keeps
// showing what it showed until it is refreshed with Ctrl+U.
const Expiration = cache.NoExpiration

// NewPagesRegistry creates a new pages registry.
func NewPagesRegistry(_ *config.ColorConfig) *PagesRegistry {
	registry := &PagesRegistry{
		UI:              &UI{Pages: tview.NewPages()},
		PageMenuMap:     make(map[string]string),
		SearchablePages: []string{},
		Closers:         make(map[string]io.Closer),
		Ascend:          make(map[string]func()),
		Descend:         make(map[string]func()),
	}

	registry.SetupPageMenus()

	return registry
}

// SetupPageMenus registers the menus of the pages that exist for the whole session. Pages
// opened on demand register theirs as they are added.
func (pr *PagesRegistry) SetupPageMenus() {
	pr.PageMenuMap[Profiles] = ProfilesPageMenu
	pr.PageMenuMap[Resources] = ResourcesPageMenu
	pr.PageMenuMap[Record] = RecordPageMenu
	pr.PageMenuMap[Help] = HelpPageMenu
}

func (app *App) CheckInCache(name string, onAbsent func()) {
	_, found := app.Cache.Get(name)
	if found {
		app.SwitchToPage(name)
	} else {
		onAbsent()
	}
}

func (app *App) AddToPagesRegistry(
	name string,
	component tview.Primitive,
	menu string,
	searchable bool,
) {
	registry := app.Layout.PagesRegistry
	registry.PageMenuMap[name] = menu

	// A re-opened page must be removed from Pages before it can be re-added below.
	if registry.UI.Pages.HasPage(name) {
		registry.UI.Pages.RemovePage(name)
	}

	if searchable && !registry.isPageSearchable(name) {
		registry.SearchablePages = append(registry.SearchablePages, name)
	}

	app.Cache.Set(name, name, Expiration)

	type titledPrimitive interface {
		GetTitle() string
		SetTitle(string) *tview.Box
	}
	if t, ok := component.(titledPrimitive); ok {
		ts := time.Now().Format("2006-01-02T15:04:05")
		t.SetTitle(strings.TrimRight(t.GetTitle(), " ") + " [" + ts + "] ")
	}

	app.Layout.Menu.SetMenu(menu)
	registry.UI.Pages.AddAndSwitchToPage(name, component, true)
}

// SetPageMenu changes the keybinding bar a page shows, and redraws it when that page is in front.
// It is how a page whose keys depend on its state — an objects level with a batch left to load —
// keeps the bar honest.
func (app *App) SetPageMenu(name, menu string) {
	registry := app.Layout.PagesRegistry
	registry.PageMenuMap[name] = menu

	if current, _ := registry.UI.Pages.GetFrontPage(); current == name {
		app.Layout.Menu.SetMenu(menu)
	}
}

// SetPageNavigation records what <h> and <l> do on a page: up opens the level above it, down
// the level below. Either is nil on a page that has no such level, which is what leaves the key
// unhandled there rather than doing nothing.
func (pr *PagesRegistry) SetPageNavigation(name string, up, down func()) {
	// A page rebuilt in place — Ctrl+U on a viewer — registers again, so a half that is nil
	// this time has to clear what the previous build left.
	delete(pr.Ascend, name)
	delete(pr.Descend, name)

	if up != nil {
		pr.Ascend[name] = up
	}
	if down != nil {
		pr.Descend[name] = down
	}
}

// isPageSearchable checks if a page is in the searchable pages list.
func (pr *PagesRegistry) isPageSearchable(name string) bool {
	for _, p := range pr.SearchablePages {
		if p == name {
			return true
		}
	}
	return false
}

// Ascend opens the level above the page in front, and reports whether that page has one.
func (app *App) Ascend() bool {
	registry := app.Layout.PagesRegistry
	currentPage, _ := registry.UI.Pages.GetFrontPage()

	up, ok := registry.Ascend[currentPage]
	if !ok {
		return false
	}
	up()
	return true
}

// Descend opens the level below the page in front, and reports whether that page has one. What
// is below a table is what the cursor is on, so a table with no row under the cursor reports
// true and opens nothing: the page has a level below it, this row is just not it.
func (app *App) Descend() bool {
	registry := app.Layout.PagesRegistry
	currentPage, _ := registry.UI.Pages.GetFrontPage()

	down, ok := registry.Descend[currentPage]
	if !ok {
		return false
	}
	down()
	return true
}

func (app *App) SwitchToPage(name string) {
	if menu, ok := app.Layout.PagesRegistry.PageMenuMap[name]; ok {
		app.Layout.Menu.SetMenu(menu)
		app.Layout.PagesRegistry.UI.Pages.SwitchToPage(name)
	}
}

func (app *App) ShowModalPage(pageName string) {
	registry := app.Layout.PagesRegistry
	if menu, ok := registry.PageMenuMap[pageName]; ok {
		app.Layout.Menu.SetMenu(menu)
		registry.UI.Pages.ShowPage(pageName)
		registry.UI.Pages.SendToFront(pageName)
	}
}

func (app *App) HideModalPage(pageName string) {
	registry := app.Layout.PagesRegistry
	registry.UI.Pages.HidePage(pageName)

	currentPage, _ := registry.UI.Pages.GetFrontPage()
	if menu, ok := registry.PageMenuMap[currentPage]; ok {
		app.Layout.Menu.SetMenu(menu)
	}
}

// SetPageCloser records what a page holds open, to be closed when the page is removed. A page
// that holds nothing open registers nothing.
func (pr *PagesRegistry) SetPageCloser(name string, closer io.Closer) {
	// A page rebuilt in place — Ctrl+U on a viewer — replaces what the old one held.
	pr.closePage(name)
	pr.Closers[name] = closer
}

// closePage closes and forgets what the named page held open, if anything.
func (pr *PagesRegistry) closePage(name string) {
	closer, ok := pr.Closers[name]
	if !ok {
		return
	}
	delete(pr.Closers, name)

	if err := closer.Close(); err != nil {
		log.Error().Err(err).Str("page", name).Msg("failed closing what a page held open")
	}
}

// CloseAll closes what every page holds open. It is called when the application exits.
func (pr *PagesRegistry) CloseAll() {
	for name := range pr.Closers {
		pr.closePage(name)
	}
}

func (app *App) IsCurrentPageSearchable() bool {
	currentPage, _ := app.Layout.PagesRegistry.UI.Pages.GetFrontPage()

	for _, searchablePage := range app.Layout.PagesRegistry.SearchablePages {
		if currentPage == searchablePage {
			return true
		}
	}
	return false
}
