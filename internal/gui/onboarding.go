package gui

import (
	"fmt"
	"log"
	"net/url"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/config"
)

// First-run setup: one field and one button.
//
// The payout address is the only thing kind-miner cannot choose for the user —
// it has no default, and coins sent to a wrong one are gone. Everything the
// v0.1 wizard used to ask after it (which node, which sidechain, which
// binaries, how kind) has a default that is right for nearly everyone: the
// automatic node over Tor, nano, the bundled binaries, Polite, pause on
// battery. A setup flow that asks five questions gets clicked through without
// being read, so it asks one, and the rest lives in the settings window.
//
// The one thing that is not asked but is disclosed is starting with the
// computer. It is on by default — a miner that has to be remembered does not
// mine — but a program that quietly adds itself to login is exactly the
// behaviour a miner is suspected of, so the checkbox says so in plain words,
// right under the field, before anything happens.
//
// The screen lives in the single shared application window (see app.go) by
// swapping window content — the GUI never creates a second Fyne app. The
// headless counterpart is internal/config.RunWizard.
type onboarding struct {
	u   *uiApp
	cfg *config.Config

	wallet   *widget.Entry
	feedback *canvas.Text
	startup  *widget.Check
	node     *widget.RadioGroup
	start    *widget.Button
	object   fyne.CanvasObject
}

// onboardingScreen starts the first-run flow.
func (u *uiApp) onboardingScreen() fyne.CanvasObject {
	o := &onboarding{u: u, cfg: u.sup.Config()}
	o.build()
	return o.object
}

func (o *onboarding) build() {
	o.wallet = widget.NewEntry()
	o.wallet.SetPlaceHolder(WalletPlaceholder)
	o.wallet.SetText(o.cfg.Wallet)
	o.feedback = canvas.NewText("", colorMuted)
	o.feedback.TextSize = textSize(12)

	o.startup = widget.NewCheck(OnboardStartupLabel, nil)
	o.startup.SetChecked(true)

	o.node = widget.NewRadioGroup([]string{nodeAutomatic, nodeOwn}, nil)
	if o.cfg.Mode == config.ModeP2PoolLocal {
		o.node.Selected = nodeOwn
	} else {
		o.node.Selected = nodeAutomatic
	}
	more := widget.NewAccordion(widget.NewAccordionItem(OnboardMoreOptions,
		container.NewVBox(
			sectionLabel("Monero node"),
			o.node,
			note(OnboardNodeNote),
			note(OnboardMoreLater),
		)))

	o.start = widget.NewButton(OnboardStart, o.finish)
	o.start.Importance = widget.HighImportance
	o.wallet.OnChanged = func(string) { o.validateWallet() }
	o.wallet.OnSubmitted = func(string) {
		if ValidateAddress(o.wallet.Text) == "" {
			o.finish()
		}
	}
	o.validateWallet()

	help := container.NewHBox(
		widget.NewLabel(WalletHelp),
		widget.NewHyperlink(WalletHelpLink, parseURL(WalletHelpURL)),
	)
	// The disclosure sits directly under the field, above everything that
	// can scroll: it has to be read before Start is pressed, not found later.
	body := container.NewVBox(
		heading(OnboardWalletTitle),
		note(OnboardWalletBody),
		sectionLabel("Monero payout address"),
		o.wallet,
		o.feedback,
		o.startup,
		more,
		widget.NewSeparator(),
		note(OnboardWalletNote),
		help,
	)
	footer := container.NewBorder(nil, nil, nil, o.start)
	o.object = container.NewPadded(container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), footer),
		nil, nil,
		container.NewVScroll(body),
	))
}

// validateWallet drives the live feedback under the address field and gates
// the Start button. The address is the one thing with no default and no way to
// recover from getting wrong, so it is checked as it is typed.
func (o *onboarding) validateWallet() {
	text := o.wallet.Text
	msg := ValidateAddress(text)
	switch {
	case text == "":
		o.feedback.Text = ""
		o.feedback.Color = colorMuted
		o.start.Disable()
	case msg == "":
		o.feedback.Text = "✓ " + WalletOK
		o.feedback.Color = colorOK
		o.start.Enable()
	default:
		o.feedback.Text = "× " + msg
		o.feedback.Color = colorError
		o.start.Disable()
	}
	o.feedback.Refresh()
}

// answers applies what the screen collected to cfg. Everything it does not ask
// about keeps the value config.Defaults gave it.
func (o *onboarding) answers(cfg *config.Config) {
	cfg.Wallet = strings.TrimSpace(o.wallet.Text)
	if o.node.Selected == nodeOwn {
		cfg.Mode = config.ModeP2PoolLocal
	} else {
		cfg.Mode = config.ModeP2PoolRemote
	}
	cfg.RunAtStartup = o.startup.Checked
}

// finish saves the answers, registers the login entry the checkbox promised,
// and hands off to startup — after which the window tucks itself into the tray
// (see runStartup).
func (o *onboarding) finish() {
	if ValidateAddress(o.wallet.Text) != "" {
		return
	}
	o.answers(o.cfg)
	if err := o.cfg.Save(); err != nil {
		o.u.win.SetContent(o.u.errorScreen(fmt.Errorf("saving config: %w", err)))
		return
	}
	if o.cfg.RunAtStartup {
		// Not starting at login is a nuisance, not a reason to refuse to mine;
		// EnsureAutostart retries on the next launch.
		if err := SetAutostart(true); err != nil {
			log.Printf("autostart: could not register login entry: %v", err)
		}
	}
	o.u.tuckAfterStart = true
	o.u.startStartup()
}

// ---- shared bits ----

const (
	nodeAutomatic = "Automatic — our node over Tor, your IP stays hidden"
	nodeOwn       = "My own node — fastest templates, nothing leaves the machine"
)

func heading(text string) fyne.CanvasObject {
	t := canvas.NewText(text, colorForeground)
	t.TextSize = textSize(19)
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
