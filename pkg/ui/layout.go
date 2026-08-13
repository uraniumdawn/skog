// Copyright (c) Sergey Petrovsky
// This source code is licensed under the MIT license found in the
// LICENSE file in the root directory of this source tree.

package ui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"

	"github.com/uraniumdawn/skog/pkg/awscfg"
	"github.com/uraniumdawn/skog/pkg/config"
)

type Layout struct {
	PagesRegistry *PagesRegistry
	Profile       *tview.Table
	Search        map[string]*tview.InputField
	Content       *tview.Flex
	Header        *tview.Flex
	Menu          *Menu
	Colors        *config.ColorConfig
	StatusLine    *tview.TextView
	StatusHint    *tview.TextView
	StatusBar     *tview.Flex
}

type Borders struct {
	Horizontal  rune
	Vertical    rune
	TopLeft     rune
	TopRight    rune
	BottomLeft  rune
	BottomRight rune

	LeftT   rune
	RightT  rune
	TopT    rune
	BottomT rune
	Cross   rune

	HorizontalFocus  rune
	VerticalFocus    rune
	TopLeftFocus     rune
	TopRightFocus    rune
	BottomLeftFocus  rune
	BottomRightFocus rune
}

const (
	headerHeight   = 2
	mainProportion = 15
	searchHeight   = 3
)

// hintText is the right-hand side of the status bar, shown whenever there is no status
// message to display.
func hintText() string {
	return " skog v" + Version + " "
}

func NewLayout(registry *PagesRegistry, colors *config.ColorConfig) *Layout {
	InitBorders()

	profile := tview.NewTable()
	profile.SetTitleAlign(tview.AlignLeft)
	profile.SetBackgroundColor(tcell.GetColor(colors.Skog.Profile.BgColor))
	profile.SetSelectable(false, false)

	profile.SetCell(0, 0, tview.NewTableCell(" Profile:").
		SetTextColor(tcell.GetColor(colors.Skog.Label.FgColor)).
		SetBackgroundColor(tcell.GetColor(colors.Skog.Profile.BgColor)).
		SetExpansion(0))
	profile.SetCell(0, 1, tview.NewTableCell("").
		SetTextColor(tcell.GetColor(colors.Skog.Profile.FgColor)).
		SetBackgroundColor(tcell.GetColor(colors.Skog.Profile.BgColor)).
		SetExpansion(1))

	menu := NewMenu(colors)
	header := tview.NewFlex()
	header.SetDirection(tview.FlexColumn)

	context := tview.NewFlex()
	context.SetBorder(false)
	context.SetDirection(tview.FlexColumn)
	context.AddItem(profile, 0, 1, false)
	context.AddItem(menu.Flex, 0, 3, false)

	header.AddItem(context, 0, 3, false)

	statusLine := tview.NewTextView()
	statusLine.SetDynamicColors(true)
	statusLine.SetWrap(false)
	statusLine.SetTextAlign(tview.AlignLeft)
	statusLine.SetBackgroundColor(tcell.GetColor(colors.Skog.Status.BgColor))
	statusLine.SetTextColor(tcell.GetColor(colors.Skog.Status.FgColor))

	statusLabel := tview.NewTextView()
	statusLabel.SetDynamicColors(true)
	statusLabel.SetText(" » ")
	statusLabel.SetTextAlign(tview.AlignLeft)
	statusLabel.SetBackgroundColor(tcell.GetColor(colors.Skog.Status.BgColor))
	statusLabel.SetTextColor(tcell.GetColor(colors.Skog.Label.FgColor))

	hint := hintText()

	statusHint := tview.NewTextView()
	statusHint.SetDynamicColors(true)
	statusHint.SetText(hint)
	statusHint.SetTextAlign(tview.AlignRight)
	statusHint.SetBackgroundColor(tcell.GetColor(colors.Skog.Status.BgColor))
	statusHint.SetTextColor(tcell.GetColor(colors.Skog.Label.FgColor))

	statusBar := tview.NewFlex().
		SetDirection(tview.FlexColumn).
		AddItem(statusLabel, 3, 0, false).
		AddItem(statusLine, 0, 1, false).
		AddItem(statusHint, len([]rune(hint)), 0, false)

	main := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(header, headerHeight, 0, false).
		AddItem(registry.UI.Pages, 0, mainProportion, true).
		AddItem(statusBar, 1, 0, false)

	return &Layout{
		PagesRegistry: registry,
		Profile:       profile,
		Search:        make(map[string]*tview.InputField),
		Menu:          menu,
		Content:       main,
		Header:        header,
		Colors:        colors,
		StatusLine:    statusLine,
		StatusHint:    statusHint,
		StatusBar:     statusBar,
	}
}

func InitBorders() {
	tview.Borders = Borders{
		Horizontal:  tview.BoxDrawingsLightHorizontal,
		Vertical:    tview.DitheringNone,
		TopLeft:     tview.BoxDrawingsLightDownAndRight,
		TopRight:    tview.BoxDrawingsLightDownAndLeft,
		BottomLeft:  tview.BoxDrawingsLightUpAndRight,
		BottomRight: tview.BoxDrawingsLightUpAndLeft,

		LeftT:   tview.BoxDrawingsLightVerticalAndRight,
		RightT:  tview.BoxDrawingsLightVerticalAndLeft,
		TopT:    tview.BoxDrawingsLightDownAndHorizontal,
		BottomT: tview.BoxDrawingsLightUpAndHorizontal,
		Cross:   tview.BoxDrawingsLightVerticalAndHorizontal,

		HorizontalFocus:  tview.BoxDrawingsLightHorizontal,
		VerticalFocus:    tview.DitheringNone,
		TopLeftFocus:     tview.BoxDrawingsLightDownAndRight,
		TopRightFocus:    tview.BoxDrawingsLightDownAndLeft,
		BottomLeftFocus:  tview.BoxDrawingsLightUpAndRight,
		BottomRightFocus: tview.BoxDrawingsLightUpAndLeft,
	}
}

// SetSelected shows the name of the selected profile in the header. The mode it is worked with is
// not shown here but in the content area's border, where it stays in sight on every page — see
// drawModeBadge.
func (l *Layout) SetSelected(profile *awscfg.Profile) {
	text := ""
	if profile != nil {
		text = profile.Name
	}
	l.Profile.GetCell(0, 1).SetText(text)
}
