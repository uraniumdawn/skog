// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// IsKey reports whether the event is a printable rune key matching r.
func IsKey(event *tcell.EventKey, r rune) bool {
	return event.Key() == tcell.KeyRune && event.Rune() == r
}

// IsCtrlEnter reports whether the event is Enter with the Ctrl modifier held.
func IsCtrlEnter(event *tcell.EventKey) bool {
	return event.Key() == tcell.KeyEnter && event.Modifiers()&tcell.ModCtrl != 0
}

func (app *App) MainOperationKeyHandler() {
	app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		// tview runs the application-wide capture before the focused primitive's own, so
		// returning nil here is what blocks the application while a question stands.
		if app.answer(event) {
			return nil
		}

		if IsKey(event, ':') {
			if !app.IsSearchInFocus() && !app.IsInputFieldInFocus() {
				currentPage, _ := app.Layout.PagesRegistry.UI.Pages.GetFrontPage()
				if currentPage != Resources {
					app.ShowModalPage(Resources)
					return nil
				}
				// modal already open: fall through so table's SetInputCapture receives ':' and focuses
				// search
			}
		}

		// <?> is answered here and nowhere else: it is the one key that has to work on every
		// page, including over a modal, which is where a user reaches for it.
		if IsKey(event, '?') && !app.IsSearchInFocus() && !app.IsInputFieldInFocus() {
			currentPage, _ := app.Layout.PagesRegistry.UI.Pages.GetFrontPage()
			if currentPage == Help {
				app.HideHelp()
			} else {
				app.ShowHelp()
			}
			return nil
		}

		if IsKey(event, '/') {
			currentPage, _ := app.Layout.PagesRegistry.UI.Pages.GetFrontPage()
			for _, searchablePage := range app.Layout.PagesRegistry.SearchablePages {
				if currentPage == searchablePage {
					if _, ok := app.Layout.Search[currentPage]; ok {
						app.Layout.ShowInlineSearch(currentPage)
						app.SetFocus(app.Layout.Search[currentPage])
						SendStatusWithDefaultTTL("")
						return nil
					}
				}
			}
		}

		// <h> and <l> walk the hierarchy the page shows: up to the level above, down into the
		// row the cursor is on. They live here rather than on each page so that the guard
		// against a focused input field, where these are letters being typed, is written once.
		//
		// A page with no level in that direction leaves the key alone rather than swallowing it:
		// a modal in front is answering to its own keys.
		if !app.IsSearchInFocus() && !app.IsInputFieldInFocus() {
			if IsKey(event, 'h') && app.Ascend() {
				return nil
			}

			if IsKey(event, 'l') && app.Descend() {
				return nil
			}
		}

		return event
	})
}

func (app *App) SearchKeyHandler(input *tview.InputField) {
	input.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEnter {
			app.Layout.HideInlineSearch()
			app.SetFocus(app.Layout.PagesRegistry.UI.Pages)
		}

		if event.Key() == tcell.KeyEsc {
			app.Layout.HideInlineSearch()
			app.SetFocus(app.Layout.PagesRegistry.UI.Pages)
			return nil
		}

		return event
	})
}
