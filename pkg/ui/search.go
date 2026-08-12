// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/config"
)

// NewInlineSearch creates the inline search input field shown below the page header.
func NewInlineSearch(colors *config.ColorConfig) *tview.InputField {
	search := tview.NewInputField()
	search.SetTitleAlign(tview.AlignLeft)
	search.SetLabel(" / ")
	search.SetLabelColor(tcell.GetColor(colors.Skog.Search.FgColor))
	search.SetFieldTextColor(tcell.GetColor(colors.Skog.Search.FgColor))
	search.SetFieldBackgroundColor(tcell.GetColor(colors.Skog.Search.BgColor))
	search.SetBackgroundColor(tcell.GetColor(colors.Skog.Search.BgColor))
	search.SetBorder(true)
	search.SetBorderColor(tcell.GetColor(colors.Skog.Search.FgColor))
	search.SetBorderPadding(0, 0, 0, 0)
	return search
}

func (app *App) AssignSearch(onSearch func(text string)) {
	currentPage, _ := app.Layout.PagesRegistry.UI.Pages.GetFrontPage()
	search := NewInlineSearch(app.Layout.Colors)

	// Wrap the onSearch callback to save filter state
	wrappedOnSearch := func(text string) {
		// Save current filter to app state
		app.CurrentFilters[currentPage] = text

		// Call the original filter function
		onSearch(text)
	}

	search.SetChangedFunc(wrappedOnSearch)
	app.SearchKeyHandler(search)
	app.Layout.Search[currentPage] = search

	// Restore previous filter if it exists
	if filterText, exists := app.CurrentFilters[currentPage]; exists && filterText != "" {
		search.SetText(filterText)
		// SetText will trigger wrappedOnSearch automatically
	}
}

func (app *App) IsSearchInFocus() bool {
	for _, i := range app.Layout.Search {
		if i.HasFocus() {
			return true
		}
	}
	return false
}

func (app *App) IsInputFieldInFocus() bool {
	focused := app.GetFocus()
	if focused == nil {
		return false
	}
	_, isInputField := focused.(*tview.InputField)
	_, isTextArea := focused.(*tview.TextArea)
	return isInputField || isTextArea
}

func (l *Layout) ShowInlineSearch(currentPage string) {
	l.Content.Clear()
	l.Content.AddItem(l.Header, headerHeight, 0, false)
	l.Content.AddItem(l.Search[currentPage], searchHeight, 0, false)
	l.Content.AddItem(l.PagesRegistry.UI.Pages, 0, mainProportion, true)
	l.Content.AddItem(l.StatusBar, 1, 0, false)
}

func (l *Layout) HideInlineSearch() {
	l.Content.Clear()
	l.Content.AddItem(l.Header, headerHeight, 0, false)
	l.Content.AddItem(l.PagesRegistry.UI.Pages, 0, mainProportion, true)
	l.Content.AddItem(l.StatusBar, 1, 0, false)
}
