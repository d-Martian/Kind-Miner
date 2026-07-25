package gui

import (
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/config"
)

// showSettings opens a window with the editable configuration. Throttle
// sensitivity, battery, temperature, and thread settings apply immediately via
// Scheduler.UpdateRuntime; wallet and mode changes are saved but take effect on
// the next restart.
func (u *uiApp) showSettings() {
	if u.settingsWin != nil {
		u.settingsWin.Show()
		u.settingsWin.RequestFocus()
		return
	}

	cfg := u.sup.Config()

	w := u.app.NewWindow("kind-miner — settings")
	w.Resize(fyne.NewSize(480, 440))
	w.CenterOnScreen()
	u.settingsWin = w
	w.SetOnClosed(func() { u.settingsWin = nil })

	wallet := widget.NewEntry()
	wallet.SetText(cfg.Wallet)

	mode := widget.NewSelect([]string{
		string(config.ModeP2PoolRemote),
		string(config.ModeP2PoolLocal),
		string(config.ModePool),
	}, nil)
	mode.SetSelected(string(cfg.Mode))

	chain := widget.NewSelect([]string{"mini", "main"}, nil)
	if cfg.P2PoolChain == "main" {
		chain.SetSelected("main")
	} else {
		chain.SetSelected("mini")
	}

	sensitivity := widget.NewSelect([]string{
		string(config.SensitivityLow),
		string(config.SensitivityMedium),
		string(config.SensitivityHigh),
	}, nil)
	sensitivity.SetSelected(string(cfg.ThrottleSensitivity))

	battery := widget.NewCheck("", nil)
	battery.SetChecked(cfg.PauseOnBattery)

	startup := widget.NewCheck("", nil)
	startup.SetChecked(cfg.RunAtStartup)

	temp := widget.NewEntry()
	temp.SetText(strconv.FormatFloat(cfg.TempLimitCelsius, 'f', -1, 64))

	threads := widget.NewEntry()
	threads.SetText(strconv.Itoa(cfg.MaxThreads))

	chainItem := widget.NewFormItem("P2Pool sidechain", chain)
	chainItem.HintText = "mini suits most desktops (< ~50 kH/s); main is for higher-hashrate machines"

	startupItem := widget.NewFormItem("Run at startup", startup)
	startupItem.HintText = "kind-miner only earns while it is running"

	form := widget.NewForm(
		widget.NewFormItem("Monero wallet", wallet),
		widget.NewFormItem("Mode", mode),
		chainItem,
		startupItem,
		widget.NewFormItem("Throttle sensitivity", sensitivity),
		widget.NewFormItem("Pause on battery", battery),
		widget.NewFormItem("Temp limit (°C)", temp),
		widget.NewFormItem("Max threads (0 = auto)", threads),
	)

	status := canvas.NewText("", colorMuted)
	status.TextSize = 12
	fail := func(msg string) {
		status.Text = "× " + msg
		status.Color = colorError
		status.Refresh()
	}

	save := widget.NewButton("Save", func() {
		if msg := ValidateAddress(wallet.Text); msg != "" {
			fail(msg)
			return
		}
		tempVal, err := strconv.ParseFloat(strings.TrimSpace(temp.Text), 64)
		if err != nil {
			fail("Temp limit must be a number")
			return
		}
		threadsVal, err := strconv.Atoi(strings.TrimSpace(threads.Text))
		if err != nil {
			fail("Max threads must be a whole number")
			return
		}

		// Register with the OS before saving, so a stored preference never
		// claims a login entry the system refused to create.
		if startup.Checked != cfg.RunAtStartup {
			if err := SetAutostart(startup.Checked); err != nil {
				fail("Could not change the startup setting: " + err.Error())
				return
			}
		}

		needRestart := wallet.Text != cfg.Wallet ||
			string(cfg.Mode) != mode.Selected ||
			cfg.P2PoolChain != chain.Selected

		cfg.Wallet = strings.TrimSpace(wallet.Text)
		cfg.Mode = config.Mode(mode.Selected)
		cfg.P2PoolChain = chain.Selected
		cfg.ThrottleSensitivity = config.Sensitivity(sensitivity.Selected)
		cfg.PauseOnBattery = battery.Checked
		cfg.TempLimitCelsius = tempVal
		cfg.MaxThreads = threadsVal
		cfg.RunAtStartup = startup.Checked

		if err := cfg.Validate(); err != nil {
			fail(err.Error())
			return
		}
		if err := cfg.Save(); err != nil {
			fail(err.Error())
			return
		}

		// Apply the live-tunable settings to the running scheduler.
		if s := u.sup.Scheduler(); s != nil {
			s.UpdateRuntime(cfg)
		}
		u.updateDashboard()

		if needRestart {
			status.Text = "Saved. Restart kind-miner to apply the wallet, mode, or sidechain change."
			status.Color = colorOK
			status.Refresh()
			return
		}
		w.Close()
	})

	cancel := widget.NewButton("Cancel", func() { w.Close() })
	openCfg := widget.NewButton("Open config file", func() { openInEditor(config.Path()) })
	openDir := widget.NewButton("Open data folder", func() { openFolder(config.Dir()) })

	advanced := container.NewHBox(openCfg, openDir)
	actions := container.NewHBox(layoutSpacer(), cancel, save)

	body := container.NewVBox(form, status, widget.NewSeparator(), advanced)
	w.SetContent(container.NewBorder(nil, actions, nil, nil, container.NewPadded(body)))
	w.Show()
}
