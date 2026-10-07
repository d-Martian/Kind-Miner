package gui

import (
	"image/color"
	"math"

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
	// colorWarn is amber, kept visibly yellower than the Monero-orange accent so
	// "something is wrong" never reads as "mining".
	colorWarn = color.NRGBA{R: 0xff, G: 0xb3, B: 0x00, A: 0xff}

	// colorDivider outlines cards and separates sections. Barely there by
	// design: the dashboard should read as one surface with structure, not as a
	// grid of boxes.
	colorDivider = color.NRGBA{R: 0x2e, G: 0x33, B: 0x3b, A: 0xff}
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

// bodyText is the body text size. Fyne's default is 14, and people found it
// too small to read comfortably — notes and labels most of all — so every
// size of text in the app is scaled from it together: the theme's own body,
// caption and heading sizes, and each size set by hand (textSize), so the
// app grows without anything losing its place in the hierarchy.
const bodyText = 16

// textScale is how much larger than designed: the sizes in the code were
// chosen against Fyne's 14.
const textScale = bodyText / 14.0

// textSize is a hand-set text size, as designed against 14-point body text,
// at the size the app now uses.
func textSize(designed float32) float32 {
	return float32(math.Round(float64(designed) * textScale))
}

func (kindTheme) Size(name fyne.ThemeSizeName) float32 {
	switch name {
	case theme.SizeNameText:
		return bodyText
	case theme.SizeNameCaptionText, theme.SizeNameSubHeadingText, theme.SizeNameHeadingText:
		return textSize(theme.DefaultTheme().Size(name))
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
