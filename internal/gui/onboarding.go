package gui

import (
	"net/url"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// This file builds the first-run onboarding screens. They are shown inside the
// single shared application window (see app.go) by swapping window content —
// the GUI never creates a second Fyne app. The headless/terminal counterpart
// lives in internal/config.RunWizard.

// welcomeScreen introduces kind-miner and hands off to wallet entry when the
// user continues.
func welcomeScreen(onContinue func()) fyne.CanvasObject {
	title := canvas.NewText(WelcomeTitle, colorForeground)
	title.TextSize = 30
	title.TextStyle = fyne.TextStyle{Bold: true}

	subtitle := canvas.NewText(WelcomeSubtitle, colorAccent)
	subtitle.TextSize = 16

	body := widget.NewLabel(WelcomeBody)
	body.Wrapping = fyne.TextWrapWord
	body.Alignment = fyne.TextAlignCenter

	cont := widget.NewButton(WelcomeContinue, onContinue)
	cont.Importance = widget.HighImportance

	cta := container.NewHBox(layoutSpacer(), cont, layoutSpacer())

	form := container.NewVBox(
		layoutSpacer(),
		container.NewCenter(title),
		container.NewCenter(subtitle),
		widget.NewLabel(""),
		body,
		layoutSpacer(),
	)
	return container.NewBorder(nil, container.NewVBox(cta, widget.NewLabel("")), nil, nil,
		container.NewPadded(form))
}

// walletScreen builds the wallet-entry screen. onSubmit receives a
// syntactically valid Monero address when the user starts mining.
//
// Layout:
//
//	┌──────────────────────────────────────┐
//	│  Where should we send your coins?    │  ← title
//	│  Paste your Monero wallet address…   │  ← prompt
//	│  ┌────────────────────────────────┐  │
//	│  │ 4… or 8… (95 characters)       │  │  ← entry
//	│  └────────────────────────────────┘  │
//	│  ✓ Looks like a valid Monero address │  ← live feedback
//	│  Don't have a wallet yet? Get one →  │  ← help link
//	│              [ Start mining ]        │  ← CTA (disabled until valid)
//	└──────────────────────────────────────┘
func walletScreen(w fyne.Window, onSubmit func(string)) fyne.CanvasObject {
	_ = w // reserved for future dialogs anchored to the window

	title := canvas.NewText(WalletTitle, colorForeground)
	title.TextSize = 22
	title.TextStyle = fyne.TextStyle{Bold: true}

	prompt := canvas.NewText(WalletPrompt, colorMuted)
	prompt.TextSize = 14

	entry := widget.NewEntry()
	entry.SetPlaceHolder(WalletPlaceholder)

	feedback := canvas.NewText("", colorMuted)
	feedback.TextSize = 13

	start := widget.NewButton(WalletStart, nil)
	start.Importance = widget.HighImportance
	start.Disable()

	submit := func() {
		if msg := ValidateAddress(entry.Text); msg == "" {
			onSubmit(entry.Text)
		}
	}
	start.OnTapped = submit
	entry.OnSubmitted = func(string) { submit() }

	entry.OnChanged = func(s string) {
		msg := ValidateAddress(s)
		switch {
		case s == "":
			feedback.Text = ""
			feedback.Color = colorMuted
			start.Disable()
		case msg == "":
			feedback.Text = "✓ " + WalletOK
			feedback.Color = colorOK
			start.Enable()
		default:
			feedback.Text = "× " + msg
			feedback.Color = colorError
			start.Disable()
		}
		feedback.Refresh()
	}

	help := widget.NewHyperlink(WalletHelpLink, parseURL(WalletHelpURL))
	helpLine := container.NewHBox(
		widget.NewLabel(WalletHelp),
		help,
	)

	form := container.NewVBox(
		title,
		prompt,
		widget.NewLabel(""), // spacer
		entry,
		feedback,
		widget.NewLabel(""), // spacer
		helpLine,
	)

	cta := container.NewHBox(
		layoutSpacer(),
		start,
		layoutSpacer(),
	)

	return container.NewBorder(
		nil,
		container.NewVBox(cta, widget.NewLabel("")),
		nil, nil,
		container.NewPadded(form),
	)
}

func parseURL(raw string) *url.URL {
	u, _ := url.Parse(raw)
	return u
}

// layoutSpacer returns a flexible spacer that absorbs extra space in a box
// container — used to centre (HBox/VBox spacers on both sides) or right-align
// (a single leading spacer) content. An empty widget.Label would not expand.
func layoutSpacer() fyne.CanvasObject {
	return layout.NewSpacer()
}
