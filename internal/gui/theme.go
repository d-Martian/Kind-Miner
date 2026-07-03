package gui

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// kindTheme is a minimal Fyne theme tuned for the kind-miner aesthetic:
// dark by default, calm contrast, no Material accents.
type kindTheme struct{}

var _ fyne.Theme = (*kindTheme)(nil)

// Brand palette. Deliberately understated — a privacy tool should feel
// trustworthy, not loud.
var (
	colorBackground = color.NRGBA{R: 0x14, G: 0x16, B: 0x1a, A: 0xff}
	colorSurface    = color.NRGBA{R: 0x1c, G: 0x1f, B: 0x24, A: 0xff}
	colorForeground = color.NRGBA{R: 0xe8, G: 0xe8, B: 0xe8, A: 0xff}
	colorMuted      = color.NRGBA{R: 0x9a, G: 0x9a, B: 0xa0, A: 0xff}
	colorAccent     = color.NRGBA{R: 0xf6, G: 0x82, B: 0x1f, A: 0xff} // Monero orange
	colorOK         = color.NRGBA{R: 0x6c, G: 0xc1, B: 0x8e, A: 0xff}
	colorError      = color.NRGBA{R: 0xe5, G: 0x6b, B: 0x6f, A: 0xff}
)

func (kindTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	switch name {
	case theme.ColorNameBackground:
		return colorBackground
	case theme.ColorNameForeground:
		return colorForeground
	case theme.ColorNamePrimary, theme.ColorNameFocus:
		return colorAccent
	case theme.ColorNameSuccess:
		return colorOK
	case theme.ColorNameError:
		return colorError
	case theme.ColorNameDisabled, theme.ColorNamePlaceHolder:
		return colorMuted
	case theme.ColorNameInputBackground, theme.ColorNameButton, theme.ColorNameMenuBackground, theme.ColorNameOverlayBackground:
		return colorSurface
	default:
		return theme.DefaultTheme().Color(name, theme.VariantDark)
	}
}

func (kindTheme) Font(style fyne.TextStyle) fyne.Resource {
	return theme.DefaultTheme().Font(style)
}

func (kindTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return theme.DefaultTheme().Icon(name)
}

func (kindTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNamePadding:
		return 10
	case theme.SizeNameInnerPadding:
		return 8
	case theme.SizeNameInlineIcon:
		return 18
	default:
		return theme.DefaultTheme().Size(name)
	}
}
