package gui

import (
	"fmt"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/kindness"
)

// The settings window, grouped the way the design groups them: payout,
// connection, binaries, kindness, graph, advanced.
//
// The split is not cosmetic. Payout and connection change what the miner does
// and need a restart; kindness and graph take effect while you watch. Keeping
// them apart is what lets the Save button be honest about which is which.

// settingsForm collects the widgets Save reads, so building a tab and applying
// it are not two lists that can drift apart.
type settingsForm struct {
	wallet   *widget.Entry
	mode     *widget.Select
	chain    *widget.Select
	nodeAddr *widget.Entry

	xmrigPath  *widget.Entry
	p2poolPath *widget.Entry
	bundled    *widget.RadioGroup

	kindness      *widget.RadioGroup
	kindnessBlurb *widget.Label
	battery       *widget.Check
	thermal       *widget.Check
	onlyLocked    *widget.Check
	tempLimit     *widget.Entry
	threads       *widget.Entry
	idleAfter     *widget.Entry
	startup       *widget.Check

	shadeHeadroom *widget.Check
	markBackoff   *widget.Check
	drawTemp      *widget.Check
	fillMiner     *widget.Check
	chartWindow   *widget.RadioGroup

	// Values as the window opened, so Save can tell the user which changes need
	// a restart. Comparing against cfg after writing to it would always say no.
	walletAtOpen string
	modeAtOpen   string
	chainAtOpen  string
	nodeAtOpen   string
}

// showSettings opens the settings window, or focuses it if already open.
func (u *uiApp) showSettings() {
	if u.settingsWin != nil {
		u.settingsWin.Show()
		u.settingsWin.RequestFocus()
		return
	}

	cfg := u.sup.Config()

	w := u.app.NewWindow("kind-miner — settings")
	w.Resize(fyne.NewSize(620, 560))
	w.CenterOnScreen()
	u.settingsWin = w
	w.SetOnClosed(func() { u.settingsWin = nil })

	f := u.newSettingsForm(cfg)

	status := canvas.NewText("", colorMuted)
	status.TextSize = 12
	fail := func(msg string) {
		status.Text = "× " + msg
		status.Color = colorError
		status.Refresh()
	}

	tabs := container.NewAppTabs(
		container.NewTabItem("Payout", padTab(f.payoutTab(u))),
		container.NewTabItem("Connection", padTab(f.connectionTab(u))),
		container.NewTabItem("Binaries", padTab(f.binariesTab())),
		container.NewTabItem("Kindness", padTab(f.kindnessTab())),
		container.NewTabItem("Graph", padTab(f.graphTab())),
		container.NewTabItem("Advanced", padTab(f.advancedTab())),
	)
	tabs.SetTabLocation(container.TabLocationLeading)

	save := widget.NewButton("Save", func() {
		if msg := f.apply(u, cfg); msg != "" {
			fail(msg)
			return
		}
		if f.needsRestart(cfg) {
			status.Text = "Saved. Restart kind-miner to apply the wallet, mode, node or sidechain change."
			status.Color = colorOK
			status.Refresh()
			return
		}
		w.Close()
	})
	save.Importance = widget.HighImportance
	cancel := widget.NewButton("Cancel", func() { w.Close() })

	actions := container.NewHBox(layoutSpacer(), cancel, save)
	w.SetContent(container.NewBorder(nil, container.NewVBox(status, actions), nil, nil, tabs))
	w.Show()
}

func padTab(o fyne.CanvasObject) fyne.CanvasObject {
	return container.NewPadded(container.NewVScroll(o))
}

func (u *uiApp) newSettingsForm(cfg *config.Config) *settingsForm {
	f := &settingsForm{}

	f.wallet = widget.NewEntry()
	f.wallet.SetText(cfg.Wallet)

	f.mode = widget.NewSelect([]string{
		string(config.ModeP2PoolRemote),
		string(config.ModeP2PoolLocal),
		string(config.ModePool),
	}, nil)
	f.mode.SetSelected(string(cfg.Mode))

	f.chain = widget.NewSelect(config.Chains, nil)
	f.chain.SetSelected(normaliseChain(cfg.P2PoolChain))

	f.nodeAddr = widget.NewEntry()
	f.nodeAddr.SetPlaceHolder("leave blank for the kind-miner Nodo, over Tor")
	f.nodeAddr.SetText(cfg.RemoteNode)

	f.xmrigPath = widget.NewEntry()
	f.xmrigPath.SetText(cfg.XMRigBinPath)
	f.p2poolPath = widget.NewEntry()
	f.p2poolPath.SetText(cfg.P2PoolBinPath)

	f.bundled = widget.NewRadioGroup([]string{binariesBundled, binariesOwn}, func(choice string) {
		own := choice == binariesOwn
		setEntryEnabled(f.xmrigPath, own)
		setEntryEnabled(f.p2poolPath, own)
	})
	// A path that was set by hand is the user having chosen their own build; a
	// path autoinstall wrote is not, so the managed bin directory is excluded.
	if usesOwnBinaries(cfg) {
		f.bundled.Selected = binariesOwn
	} else {
		f.bundled.Selected = binariesBundled
	}

	f.kindness = widget.NewRadioGroup(kindness.Options(), nil)
	f.kindness.Selected = kindness.Get(cfg.Kindness).Option()
	f.kindnessBlurb = note(kindness.Get(cfg.Kindness).Blurb)
	f.kindness.OnChanged = func(option string) {
		if level, ok := kindness.ByOption(option); ok {
			f.kindnessBlurb.SetText(kindness.Get(level).Blurb)
		}
	}

	f.battery = widget.NewCheck("Pause entirely on battery", nil)
	f.battery.SetChecked(cfg.PauseOnBattery)
	f.thermal = widget.NewCheck("Ease off as the CPU nears its temperature limit", nil)
	f.thermal.SetChecked(cfg.ThermalGovernor)
	f.onlyLocked = widget.NewCheck("Mine only when the screen is locked", nil)
	f.onlyLocked.SetChecked(cfg.MineOnlyWhenLocked)
	f.startup = widget.NewCheck("Start kind-miner when I log in", nil)
	f.startup.SetChecked(cfg.RunAtStartup)

	f.tempLimit = widget.NewEntry()
	f.tempLimit.SetText(strconv.FormatFloat(cfg.TempLimitCelsius, 'f', -1, 64))
	f.threads = widget.NewEntry()
	f.threads.SetText(strconv.Itoa(cfg.MaxThreads))
	f.idleAfter = widget.NewEntry()
	f.idleAfter.SetText(strconv.Itoa(cfg.IdleFullAfterSeconds))

	f.shadeHeadroom = widget.NewCheck("Shade the reserved headroom above the kindness ceiling", nil)
	f.shadeHeadroom.SetChecked(cfg.Chart.ShadeHeadroom)
	f.markBackoff = widget.NewCheck("Mark the moments the miner stepped aside", nil)
	f.markBackoff.SetChecked(cfg.Chart.MarkBackoff)
	f.drawTemp = widget.NewCheck("Draw CPU temperature", nil)
	f.drawTemp.SetChecked(cfg.Chart.DrawTemp)
	f.fillMiner = widget.NewCheck("Fill under the miner line", nil)
	f.fillMiner.SetChecked(cfg.Chart.FillMiner)

	f.chartWindow = widget.NewRadioGroup(chartWindowLabels(), nil)
	f.chartWindow.Horizontal = true
	f.chartWindow.Selected = chartWindowLabel(cfg.Chart.WindowSeconds)

	f.walletAtOpen = cfg.Wallet
	f.modeAtOpen = string(cfg.Mode)
	f.chainAtOpen = cfg.P2PoolChain
	f.nodeAtOpen = cfg.RemoteNode

	return f
}

// chainAdvice tells the user whether the sidechain they are on still suits the
// machine.
//
// It matters more than it looks: a miner on a chain whose share difficulty it
// cannot hit inside the payout window earns almost nothing, and the symptom —
// no payouts — looks identical to the miner being broken.
func chainAdvice(st engine.P2PoolStats, current string) string {
	shareEvery, ok := st.ShareInterval()
	if !ok {
		return "Still measuring how often this machine lands a share."
	}
	suggested, change := st.SuggestedChain(current)
	if !change {
		return fmt.Sprintf(
			"You land a share %s, which keeps you in the payout window continuously. %s suits this machine.",
			formatETA(shareEvery), current)
	}
	return fmt.Sprintf(
		"You land a share only %s, so you spend most of the payout window earning nothing. The %s chain would suit this machine better.",
		formatETA(shareEvery), suggested)
}

// ---- tabs ----

const (
	binariesBundled = "Use the bundled binaries"
	binariesOwn     = "Use binaries already on this machine"
)

func (f *settingsForm) payoutTab(u *uiApp) fyne.CanvasObject {
	facts := newKVList()
	if p := u.sup.P2Pool(); p != nil {
		if st, ok := p.Stats(); ok {
			facts.Add("Shares found", fmt.Sprintf("%d", st.SharesFound))
			facts.Add("Your share of the next block", fmt.Sprintf("%.3f%%", st.RewardSharePercent))
		}
	}

	return container.NewVBox(
		sectionLabel("Monero payout address"),
		f.wallet,
		note("Use a primary address (starts with 4). P2Pool pays straight into the block's coinbase output, so subaddresses and integrated addresses cannot receive payouts."),
		note("Changing this takes effect at the next p2pool restart. Shares already in the payout window pay to the old address."),
		facts.Object(),
	)
}

func (f *settingsForm) connectionTab(u *uiApp) fyne.CanvasObject {
	suggestion := widget.NewLabel("")
	suggestion.Wrapping = fyne.TextWrapWord
	if p := u.sup.P2Pool(); p != nil {
		if st, ok := p.Stats(); ok {
			suggestion.SetText(chainAdvice(st, u.sup.Config().P2PoolChain))
		}
	}

	return container.NewVBox(
		sectionLabel("How to reach the Monero network"),
		f.mode,
		note("p2pool-remote follows a node over Tor and needs nothing installed. p2pool-local follows a monerod you run yourself: the private option, and the one we'd pick if you have the disk for it."),
		sectionLabel("Remote node"),
		f.nodeAddr,
		note("Any monerod with ZMQ enabled. Blank uses the kind-miner Nodo, reached as a Tor hidden service so the node operator never sees your IP."),
		sectionLabel("P2Pool sidechain"),
		f.chain,
		suggestion,
	)
}

func (f *settingsForm) binariesTab() fyne.CanvasObject {
	return container.NewVBox(
		sectionLabel("Which xmrig and p2pool to run"),
		f.bundled,
		note("The bundled binaries are pinned and SHA256-verified against the manifest published with this release, and are reproducible builds: build them yourself from the pinned source and you get byte-identical files."),
		sectionLabel("xmrig path"),
		f.xmrigPath,
		sectionLabel("p2pool path"),
		f.p2poolPath,
		note("kind-miner will launch and supervise binaries you point it at, but cannot vouch for how they were built. It never runs a miner you did not choose, and never mines to any address but yours."),
	)
}

func (f *settingsForm) kindnessTab() fyne.CanvasObject {
	return container.NewVBox(
		sectionLabel("How much of the machine the miner may take, and how fast it lets go"),
		f.kindness,
		f.kindnessBlurb,
		widget.NewSeparator(),
		f.battery,
		f.thermal,
		f.onlyLocked,
		f.startup,
		sectionLabel("Full kindness after idle (seconds)"),
		f.idleAfter,
		note("Until the keyboard and mouse have been quiet this long, mining is held to the Ghost ceiling. 0 mines at the chosen kindness immediately."),
		sectionLabel("Temperature limit (°C)"),
		f.tempLimit,
		sectionLabel("Maximum threads (0 = half the machine)"),
		f.threads,
	)
}

func (f *settingsForm) graphTab() fyne.CanvasObject {
	return container.NewVBox(
		sectionLabel("What the dashboard chart draws besides the two lines"),
		f.shadeHeadroom,
		f.markBackoff,
		f.drawTemp,
		f.fillMiner,
		sectionLabel("Window"),
		f.chartWindow,
	)
}

func (f *settingsForm) advancedTab() fyne.CanvasObject {
	facts := newKVList().
		Add("Config", config.Path()).
		Add("Data", config.Dir()).
		Add("Binaries", autoinstall.BinDir())

	openCfg := widget.NewButton("Open config file", func() { openInEditor(config.Path()) })
	openDir := widget.NewButton("Open data folder", func() { openFolder(config.Dir()) })

	return container.NewVBox(
		sectionLabel("Files"),
		facts.Object(),
		container.NewHBox(openCfg, openDir),
		note("Everything above writes to one file. Anything you set there by hand is kept — kind-miner only rewrites the keys it owns."),
	)
}

// ---- applying ----

// apply validates the form and writes it to cfg. It returns an error message
// for display, or "" on success.
//
// Order matters: nothing is written to cfg until every field has parsed, so a
// rejected form leaves the running configuration exactly as it was.
func (f *settingsForm) apply(u *uiApp, cfg *config.Config) string {
	if msg := ValidateAddress(f.wallet.Text); msg != "" {
		return msg
	}
	tempVal, err := strconv.ParseFloat(strings.TrimSpace(f.tempLimit.Text), 64)
	if err != nil {
		return "Temperature limit must be a number"
	}
	threadsVal, err := strconv.Atoi(strings.TrimSpace(f.threads.Text))
	if err != nil || threadsVal < 0 {
		return "Maximum threads must be 0 or more"
	}
	idleVal, err := strconv.Atoi(strings.TrimSpace(f.idleAfter.Text))
	if err != nil || idleVal < 0 {
		return "Full kindness after idle must be 0 or more seconds"
	}
	level, ok := kindness.ByOption(f.kindness.Selected)
	if !ok {
		return "Pick a kindness preset"
	}

	// Register with the OS before saving, so a stored preference never claims a
	// login entry the system refused to create.
	if f.startup.Checked != cfg.RunAtStartup {
		if err := SetAutostart(f.startup.Checked); err != nil {
			return "Could not change the startup setting: " + err.Error()
		}
	}

	cfg.Wallet = strings.TrimSpace(f.wallet.Text)
	cfg.Mode = config.Mode(f.mode.Selected)
	cfg.P2PoolChain = f.chain.Selected
	cfg.RemoteNode = strings.TrimSpace(f.nodeAddr.Text)
	cfg.Kindness = level
	cfg.PauseOnBattery = f.battery.Checked
	cfg.ThermalGovernor = f.thermal.Checked
	cfg.MineOnlyWhenLocked = f.onlyLocked.Checked
	cfg.RunAtStartup = f.startup.Checked
	cfg.TempLimitCelsius = tempVal
	cfg.MaxThreads = threadsVal
	cfg.IdleFullAfterSeconds = idleVal

	if f.bundled.Selected == binariesOwn {
		cfg.XMRigBinPath = strings.TrimSpace(f.xmrigPath.Text)
		cfg.P2PoolBinPath = strings.TrimSpace(f.p2poolPath.Text)
	} else {
		// Clearing the paths hands resolution back to autoinstall, which finds
		// the verified bundled copies.
		cfg.XMRigBinPath = ""
		cfg.P2PoolBinPath = ""
	}

	cfg.Chart = config.ChartOptions{
		ShadeHeadroom: f.shadeHeadroom.Checked,
		MarkBackoff:   f.markBackoff.Checked,
		DrawTemp:      f.drawTemp.Checked,
		FillMiner:     f.fillMiner.Checked,
		WindowSeconds: chartWindowSeconds(f.chartWindow.Selected),
	}

	if err := cfg.Validate(); err != nil {
		return err.Error()
	}
	if err := cfg.Save(); err != nil {
		return err.Error()
	}

	if s := u.sup.Scheduler(); s != nil {
		s.UpdateRuntime(cfg)
	}
	u.updateDashboard()
	return ""
}

// needsRestart reports whether the saved change only takes effect once the
// subprocesses are relaunched. It is checked after apply, so it compares
// against the values now in cfg.
func (f *settingsForm) needsRestart(cfg *config.Config) bool {
	return cfg.Wallet != f.walletAtOpen ||
		string(cfg.Mode) != f.modeAtOpen ||
		cfg.P2PoolChain != f.chainAtOpen ||
		cfg.RemoteNode != f.nodeAtOpen
}

// ---- helpers ----

func normaliseChain(chain string) string {
	for _, c := range config.Chains {
		if c == chain {
			return c
		}
	}
	return config.ChainMini
}

// usesOwnBinaries reports whether the config points at binaries outside the
// directory autoinstall manages.
func usesOwnBinaries(cfg *config.Config) bool {
	managed := autoinstall.BinDir()
	for _, p := range []string{cfg.XMRigBinPath, cfg.P2PoolBinPath} {
		if p != "" && !strings.HasPrefix(p, managed) {
			return true
		}
	}
	return false
}

func setEntryEnabled(e *widget.Entry, enabled bool) {
	if enabled {
		e.Enable()
	} else {
		e.Disable()
	}
}

func chartWindowLabels() []string {
	out := make([]string, 0, len(config.ChartWindows))
	for _, s := range config.ChartWindows {
		out = append(out, chartWindowLabel(s))
	}
	return out
}

func chartWindowLabel(seconds int) string {
	if seconds >= 60 && seconds%60 == 0 {
		return fmt.Sprintf("%dm", seconds/60)
	}
	return fmt.Sprintf("%ds", seconds)
}

func chartWindowSeconds(label string) int {
	for _, s := range config.ChartWindows {
		if chartWindowLabel(s) == label {
			return s
		}
	}
	return config.Defaults().Chart.WindowSeconds
}
