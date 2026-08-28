package gui

import (
	"fmt"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/kindness"
)

// First-run setup, in four questions.
//
// They are the four things kind-miner genuinely cannot guess: where the money
// goes, how to reach the network, which binaries to trust, and how much of the
// machine it may take. Everything else has a defensible default and is left to
// the settings window, because a setup flow that asks fourteen questions gets
// clicked through without being read.
//
// The screens live in the single shared application window (see app.go) by
// swapping window content — the GUI never creates a second Fyne app. The
// headless counterpart is internal/config.RunWizard.

const onboardingSteps = 4

// onboarding holds the state of the setup flow across its steps.
type onboarding struct {
	u   *uiApp
	cfg *config.Config

	step int

	wallet   *widget.Entry
	feedback *canvas.Text

	mode  *widget.RadioGroup
	chain *widget.RadioGroup

	binaries   *widget.RadioGroup
	xmrigPath  *widget.Entry
	p2poolPath *widget.Entry

	kindness      *widget.RadioGroup
	kindnessBlurb *widget.Label

	stepLabel *canvas.Text
	dots      []*canvas.Rectangle
	back      *widget.Button
	next      *widget.Button
	skip      *widget.Button
	body      *fyne.Container
	object    fyne.CanvasObject
}

// onboardingScreen starts the first-run flow.
func (u *uiApp) onboardingScreen() fyne.CanvasObject {
	o := &onboarding{u: u, cfg: u.sup.Config(), step: 1}
	o.build()
	o.show()
	return o.object
}

func (o *onboarding) build() {
	o.stepLabel = canvas.NewText("", colorMuted)
	o.stepLabel.TextSize = 11

	for i := 0; i < onboardingSteps; i++ {
		dot := canvas.NewRectangle(colorDivider)
		dot.SetMinSize(fyne.NewSize(18, 3))
		dot.CornerRadius = 1.5
		o.dots = append(o.dots, dot)
	}
	dotRow := container.NewHBox()
	for _, d := range o.dots {
		dotRow.Add(container.NewCenter(d))
	}

	o.back = widget.NewButton("Back", func() { o.goTo(o.step - 1) })
	o.back.Importance = widget.LowImportance
	o.skip = widget.NewButton("Skip setup", o.finish)
	o.skip.Importance = widget.LowImportance
	o.next = widget.NewButton("Continue", o.advance)
	o.next.Importance = widget.HighImportance

	o.buildWalletStep()
	o.buildConnectionStep()
	o.buildBinariesStep()
	o.buildKindnessStep()

	o.body = container.NewMax()

	header := container.NewBorder(nil, nil, o.stepLabel, dotRow)
	footer := container.NewBorder(nil, nil, o.back, container.NewHBox(o.skip, o.next))

	o.object = container.NewPadded(container.NewBorder(
		container.NewVBox(header, widget.NewSeparator()),
		container.NewVBox(widget.NewSeparator(), footer),
		nil, nil,
		container.NewVScroll(o.body),
	))
}

// ---- steps ----

func (o *onboarding) buildWalletStep() {
	o.wallet = widget.NewEntry()
	o.wallet.SetPlaceHolder(WalletPlaceholder)
	o.wallet.SetText(o.cfg.Wallet)

	o.feedback = canvas.NewText("", colorMuted)
	o.feedback.TextSize = 12

	o.wallet.OnChanged = func(string) { o.validateWallet() }
	o.wallet.OnSubmitted = func(string) { o.advance() }
}

func (o *onboarding) walletStep() fyne.CanvasObject {
	help := container.NewHBox(
		widget.NewLabel(WalletHelp),
		widget.NewHyperlink(WalletHelpLink, parseURL(WalletHelpURL)),
	)
	return container.NewVBox(
		heading(OnboardWalletTitle),
		note(OnboardWalletBody),
		sectionLabel("Monero payout address"),
		o.wallet,
		o.feedback,
		note(OnboardWalletNote),
		help,
	)
}

func (o *onboarding) buildConnectionStep() {
	o.mode = widget.NewRadioGroup([]string{modeOwnNode, modeRemoteTor}, nil)
	if o.cfg.Mode == config.ModeP2PoolLocal {
		o.mode.Selected = modeOwnNode
	} else {
		o.mode.Selected = modeRemoteTor
	}

	o.chain = widget.NewRadioGroup(config.Chains, nil)
	o.chain.Horizontal = true
	o.chain.Selected = normaliseChain(o.cfg.P2PoolChain)
}

func (o *onboarding) connectionStep() fyne.CanvasObject {
	return container.NewVBox(
		heading(OnboardNodeTitle),
		note(OnboardNodeBody),
		o.mode,
		note(OnboardNodeNote),
		sectionLabel("P2Pool sidechain"),
		o.chain,
		note(OnboardChainNote),
	)
}

func (o *onboarding) buildBinariesStep() {
	o.xmrigPath = widget.NewEntry()
	o.xmrigPath.SetPlaceHolder("/usr/local/bin/xmrig")
	o.xmrigPath.SetText(o.cfg.XMRigBinPath)
	o.p2poolPath = widget.NewEntry()
	o.p2poolPath.SetPlaceHolder("/usr/local/bin/p2pool")
	o.p2poolPath.SetText(o.cfg.P2PoolBinPath)

	o.binaries = widget.NewRadioGroup([]string{binariesBundled, binariesOwn}, func(choice string) {
		own := choice == binariesOwn
		setEntryEnabled(o.xmrigPath, own)
		setEntryEnabled(o.p2poolPath, own)
	})
	if usesOwnBinaries(o.cfg) {
		o.binaries.Selected = binariesOwn
	} else {
		o.binaries.Selected = binariesBundled
	}
	setEntryEnabled(o.xmrigPath, o.binaries.Selected == binariesOwn)
	setEntryEnabled(o.p2poolPath, o.binaries.Selected == binariesOwn)
}

func (o *onboarding) binariesStep() fyne.CanvasObject {
	return container.NewVBox(
		heading(OnboardBinariesTitle),
		note(OnboardBinariesBody),
		o.binaries,
		sectionLabel("xmrig path"),
		o.xmrigPath,
		sectionLabel("p2pool path"),
		o.p2poolPath,
		note(OnboardBinariesNote),
	)
}

func (o *onboarding) buildKindnessStep() {
	o.kindnessBlurb = note(kindness.Get(o.cfg.Kindness).Blurb)
	o.kindness = widget.NewRadioGroup(kindness.Options(), func(option string) {
		if level, ok := kindness.ByOption(option); ok {
			o.kindnessBlurb.SetText(kindness.Get(level).Blurb)
		}
	})
	o.kindness.Selected = kindness.Get(o.cfg.Kindness).Option()
}

func (o *onboarding) kindnessStep() fyne.CanvasObject {
	return container.NewVBox(
		heading(OnboardKindnessTitle),
		note(OnboardKindnessBody),
		o.kindness,
		o.kindnessBlurb,
		note(OnboardKindnessNote),
	)
}

// ---- flow ----

func (o *onboarding) show() {
	panes := []func() fyne.CanvasObject{
		o.walletStep, o.connectionStep, o.binariesStep, o.kindnessStep,
	}
	o.body.Objects = []fyne.CanvasObject{panes[o.step-1]()}
	o.body.Refresh()

	o.stepLabel.Text = fmt.Sprintf("Step %d of %d", o.step, onboardingSteps)
	o.stepLabel.Refresh()
	for i, dot := range o.dots {
		fill := colorDivider
		if i < o.step {
			fill = colorAccent
		}
		dot.FillColor = fill
		dot.Refresh()
	}

	if o.step == 1 {
		o.back.Hide()
		o.skip.Hide()
	} else {
		o.back.Show()
		o.skip.Show()
	}
	if o.step == onboardingSteps {
		o.next.SetText("Start mining")
	} else {
		o.next.SetText("Continue")
	}
	o.validateWallet()
}

func (o *onboarding) goTo(step int) {
	if step < 1 || step > onboardingSteps {
		return
	}
	o.step = step
	o.show()
}

func (o *onboarding) advance() {
	if o.step == 1 && ValidateAddress(o.wallet.Text) != "" {
		return
	}
	if o.step < onboardingSteps {
		o.goTo(o.step + 1)
		return
	}
	o.finish()
}

// validateWallet drives the live feedback under the address field and gates the
// Continue button on step one. The address is the one thing with no default and
// no way to recover from getting wrong — coins sent to a mistyped address are
// simply gone — so it is checked as it is typed.
func (o *onboarding) validateWallet() {
	if o.step != 1 {
		o.next.Enable()
		return
	}
	text := o.wallet.Text
	msg := ValidateAddress(text)
	switch {
	case text == "":
		o.feedback.Text = ""
		o.feedback.Color = colorMuted
		o.next.Disable()
	case msg == "":
		o.feedback.Text = "✓ " + WalletOK
		o.feedback.Color = colorOK
		o.next.Enable()
	default:
		o.feedback.Text = "× " + msg
		o.feedback.Color = colorError
		o.next.Disable()
	}
	o.feedback.Refresh()
}

// finish writes the collected answers and hands off to startup. Skipping only
// skips the questions after the address, which has already been validated.
func (o *onboarding) finish() {
	cfg := o.cfg
	cfg.Wallet = strings.TrimSpace(o.wallet.Text)

	if o.mode.Selected == modeOwnNode {
		cfg.Mode = config.ModeP2PoolLocal
	} else {
		cfg.Mode = config.ModeP2PoolRemote
	}
	cfg.P2PoolChain = normaliseChain(o.chain.Selected)

	if o.binaries.Selected == binariesOwn {
		cfg.XMRigBinPath = strings.TrimSpace(o.xmrigPath.Text)
		cfg.P2PoolBinPath = strings.TrimSpace(o.p2poolPath.Text)
	} else {
		cfg.XMRigBinPath = ""
		cfg.P2PoolBinPath = ""
	}

	if level, ok := kindness.ByOption(o.kindness.Selected); ok {
		cfg.Kindness = level
	}

	if err := cfg.Save(); err != nil {
		o.u.win.SetContent(o.u.errorScreen(fmt.Errorf("saving config: %w", err)))
		return
	}
	o.u.startStartup()
}

// ---- shared bits ----

const (
	modeOwnNode   = "Your own node — fastest templates, nothing leaves the machine"
	modeRemoteTor = "Our node, over Tor — nothing to install, your IP stays hidden"
)

func heading(text string) fyne.CanvasObject {
	t := canvas.NewText(text, colorForeground)
	t.TextSize = 19
	t.TextStyle = fyne.TextStyle{Bold: true}
	return t
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
