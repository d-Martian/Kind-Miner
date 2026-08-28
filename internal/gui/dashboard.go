package gui

import (
	"fmt"
	"image/color"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/monitor"
	"github.com/kind-miner/kind-miner/internal/scheduler"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// The dashboard: what the miner is doing, what it has earned, and — the part
// that matters most — a live picture of it getting out of the user's way.

// hashrateWindow is the averaging window for the headline hashrate. Short
// enough to move when the miner throttles, long enough not to flicker.
const hashrateWindow = 5 * time.Second

// dashboard holds the live widgets so refresh can update them in place rather
// than rebuilding the screen every second.
type dashboard struct {
	u *uiApp

	pill   *statusPill
	uptime *canvas.Text

	hash   *statTile
	income *statTile
	power  *statTile
	link   *statTile

	hashDetail   *kvList
	incomeDetail *kvList
	powerDetail  *kvList
	linkDetail   *kvList

	chart    *chart
	totalNow *canvas.Text
	backoff  *canvas.Text

	pause    *widget.Button
	mineNow  *widget.Button
	kindness *widget.RadioGroup

	object fyne.CanvasObject
}

func (u *uiApp) newDashboard() *dashboard {
	d := &dashboard{u: u}

	d.pill = newStatusPill()
	d.uptime = canvas.NewText("", colorMuted)
	d.uptime.TextSize = 11
	d.uptime.TextStyle = fyne.TextStyle{Monospace: true}

	settings := widget.NewButton("Settings", u.onSettings)
	settings.Importance = widget.LowImportance

	header := container.NewBorder(nil, nil,
		d.pill.Object(),
		container.NewHBox(d.uptime, settings),
	)

	d.hash = newStatTile("Hashrate · 5s avg")
	d.income = newStatTile("Est. XMR / day")
	d.power = newStatTile("Miner power")
	d.link = newStatTile("Node · pool")
	tiles := container.NewGridWithColumns(4,
		d.hash.Object(), d.income.Object(), d.power.Object(), d.link.Object())

	// The design puts this detail in hover tooltips. Fyne 2.5.3 has no tooltip,
	// and dropping the numbers would lose the part that lets someone check the
	// headline figures — so they live in an accordion under the tiles instead,
	// one section per tile.
	d.hashDetail = newKVList().
		Add("60s average", emDash).
		Add("Since start", emDash).
		Add("Peak", emDash).
		Add("Threads", emDash)
	d.incomeDetail = newKVList().
		Add("Est. per week", emDash).
		Add("Network hashrate", emDash).
		Add("Difficulty", emDash).
		Add("Your share of the next block", emDash)
	d.powerDetail = newKVList().
		Add("Whole package", emDash).
		Add("Efficiency", emDash).
		Add("CPU temperature", emDash)
	d.linkDetail = newKVList().
		Add("Node", emDash).
		Add("P2Pool chain", emDash).
		Add("Shares found", emDash).
		Add("You land a share", emDash).
		Add("Time in the payout window", emDash).
		Add("Next reward", emDash)

	// One collapsed row rather than four, so the chart keeps the height it
	// needs — it is the reason this window exists.
	//
	// Two columns rather than four: a collapsed accordion still contributes its
	// content's minimum width, and Fyne sizes the window to that. Four columns
	// of key/value rows set a floor well past the window's own default, so the
	// dashboard opened wider than it was designed for however it was resized.
	details := widget.NewAccordion(widget.NewAccordionItem("Details",
		container.NewGridWithColumns(2,
			detailColumn("Hashrate", d.hashDetail),
			detailColumn("Earnings", d.incomeDetail),
			detailColumn("Power", d.powerDetail),
			detailColumn("Connection", d.linkDetail),
		)))

	d.chart = newChart()
	d.totalNow = canvas.NewText("", colorMuted)
	d.totalNow.TextSize = 11
	d.totalNow.TextStyle = fyne.TextStyle{Monospace: true}
	d.backoff = canvas.NewText("", colorMuted)
	d.backoff.TextSize = 11

	legend := container.NewBorder(nil, nil,
		container.NewHBox(
			legendSwatch(colorMuted), legendText("Everything else"),
			legendSwatch(colorAccent), legendText("kind-miner"),
			legendSwatch(withAlpha(colorMuted, 0x80)), legendText("Kindness ceiling"),
		),
		container.NewHBox(d.totalNow, d.backoff),
	)

	plot := container.NewBorder(legend, nil, nil, nil, d.chart.Object())

	// Short labels here: the footer shares its row with the kindness control,
	// and the tray has the room for the fuller wording.
	d.pause = widget.NewButton(labelPause, u.onToggle)
	d.mineNow = widget.NewButton(labelMineNowShort, u.onMineNowToggle)

	d.kindness = widget.NewRadioGroup(kindness.Labels(), func(label string) {
		level, ok := kindness.ByLabel(label)
		if !ok {
			return
		}
		u.setKindness(level)
	})
	d.kindness.Horizontal = true
	d.kindness.Selected = kindness.Get(u.sup.Config().Kindness).Label

	footer := container.NewBorder(nil, nil,
		container.NewHBox(d.pause, d.mineNow),
		container.NewHBox(sectionLabel("Kindness"), d.kindness),
	)

	body := container.NewBorder(
		container.NewVBox(header, tiles),
		container.NewVBox(details, footer),
		nil, nil,
		plot,
	)
	d.object = container.NewPadded(body)
	return d
}

// detailColumn heads one group of the details panel, so a value can be found
// without reading all four columns.
func detailColumn(title string, list *kvList) fyne.CanvasObject {
	return container.NewVBox(sectionLabel(title), list.Object())
}

func legendText(s string) fyne.CanvasObject {
	t := canvas.NewText(s, colorMuted)
	t.TextSize = 11
	return t
}

// refresh pulls the current state and updates every live field. It runs once a
// second, so it must not allocate heavily or block.
func (d *dashboard) refresh() {
	sched := d.u.sup.Scheduler()
	xmrig := d.u.sup.XMRig()
	if sched == nil || xmrig == nil {
		return
	}
	cfg := d.u.sup.Config()
	history := sched.History()
	state, reason := sched.CurrentState()
	override := sched.Override()
	preset := sched.Preset()
	paused := state == scheduler.StatePaused

	d.refreshStatus(sched, state, reason, override, preset)
	d.refreshHashrate(history, sched, xmrig, paused)
	d.refreshEarnings(history)
	d.refreshPower(history)
	d.refreshConnection(cfg)
	d.refreshChart(history, cfg, preset, paused)

	d.pause.SetText(pauseLabel(override, paused))
	if override == scheduler.OverrideMine {
		d.mineNow.SetText(labelMineAutoShort)
	} else {
		d.mineNow.SetText(labelMineNowShort)
	}
	if label := preset.Label; d.kindness.Selected != label {
		d.kindness.Selected = label
		d.kindness.Refresh()
	}
}

func (d *dashboard) refreshStatus(sched *scheduler.Scheduler, state scheduler.State, reason string, override scheduler.Override, preset kindness.Preset) {
	text, tint := statusSummary(state, reason, override, preset, sched.IdleCountdown)
	d.pill.Set(text, tint)

	if up, ok := d.u.sup.Uptime(); ok {
		setText(d.uptime, "up "+formatUptime(up))
	}
}

// statusSummary renders the headline: what the miner is doing, and under which
// preset. When mining is held back, saying why is the whole point — a user who
// cannot tell why their machine feels different tends to uninstall rather than
// investigate.
//
// The one word "Paused" is reserved for the user's own pause. When the
// scheduler stood the miner down itself it says "Standing by" instead, because
// a live run showed what "Paused" does next to a "Pause mining" button: the
// header and the controls contradict each other, and the user cannot tell
// whether they are mining, or ever will be again. Standing by is a promise —
// this is temporary, and the miner comes back on its own.
func statusSummary(state scheduler.State, reason string, override scheduler.Override, preset kindness.Preset, countdown func() (int, bool)) (string, color.Color) {
	switch {
	case override == scheduler.OverridePause:
		return "Paused — nothing runs until you resume", colorMuted
	case state == scheduler.StatePaused && reason != "":
		return "Standing by · " + reason + " · resumes on its own", colorAccent
	case state == scheduler.StatePaused:
		return "Standing by", colorAccent
	case reason == scheduler.ReasonWaitingForIdle:
		if secs, ok := countdown(); ok {
			return fmt.Sprintf("Mining gently · full %s kindness in %s", preset.Label, formatCountdown(secs)), colorAccent
		}
		return "Mining gently · " + preset.Label, colorAccent
	case state == scheduler.StateFull:
		return "Mining · " + preset.Label, colorOK
	default:
		return "Mining, stepping aside · " + preset.Label, colorAccent
	}
}

// pauseLabel names what pressing the pause button will actually do, given
// where the miner already is. Offering "Pause mining" under a header that says
// the miner is paused was the contradiction that made the live dashboard
// unreadable — when the scheduler has stood the miner down on its own, the
// button's real effect is to make that pause stick until resumed, so that is
// what it says.
func pauseLabel(override scheduler.Override, paused bool) string {
	switch {
	case override == scheduler.OverridePause:
		return labelResume
	case paused:
		return labelKeepPaused
	default:
		return labelPause
	}
}

func (d *dashboard) refreshHashrate(history []stats.Sample, sched *scheduler.Scheduler, xmrig *engine.XMRig, paused bool) {
	if paused {
		// A suspended miner is doing exactly zero work. The dash means "no
		// reading", which is a different claim — and next to a paused header
		// it left "am I mining?" without a number for an answer.
		d.hash.Set("0", "H/s")
	} else {
		live := xmrig.Hashrate()
		if mean, ok := stats.Mean(history, hashrateWindow, stats.Hashrate); ok && mean > 0 {
			live = mean
		}
		value, unit := hashrateParts(live)
		d.hash.Set(value, unit)
	}

	if mean, ok := stats.Mean(history, time.Minute, stats.Hashrate); ok {
		d.hashDetail.Set("60s average", formatHashrate(mean))
	}
	if mean, ok := stats.MeanAll(history, stats.Hashrate); ok {
		d.hashDetail.Set("Since start", formatHashrate(mean))
	}
	if peak, ok := stats.Peak(history, stats.Hashrate); ok {
		d.hashDetail.Set("Peak", formatHashrate(peak))
	}
	active, total := sched.ActiveThreads()
	d.hashDetail.Set("Threads", fmt.Sprintf("%d of %d", active, total))
}

func (d *dashboard) refreshEarnings(history []stats.Sample) {
	p2pool := d.u.sup.P2Pool()
	if p2pool == nil {
		d.income.Set(emDash, "")
		return
	}
	st, ok := p2pool.Stats()
	if !ok {
		return
	}
	// The income estimate uses a long average: a five-second hashrate would
	// make the figure jump every time the miner stepped aside, implying the
	// day's earnings had changed when only the moment had.
	hashrate, _ := stats.MeanAll(history, stats.Hashrate)
	if hashrate <= 0 {
		hashrate = float64(st.MinerHashrate15m)
	}

	if perDay, ok := st.EstimatedXMRPerDay(hashrate); ok {
		d.income.Set(formatXMR(perDay, 5), "")
		d.incomeDetail.Set("Est. per week", formatXMR(perDay*7, 4)+" XMR")
	}
	if netHR, ok := st.NetworkHashrate(); ok {
		d.incomeDetail.Set("Network hashrate", formatHashrate(netHR))
	}
	if st.NetworkDifficulty > 0 {
		d.incomeDetail.Set("Difficulty", formatBigNumber(float64(st.NetworkDifficulty)))
	}
	if st.RewardSharePercent > 0 {
		d.incomeDetail.Set("Your share of the next block", fmt.Sprintf("%.3f%%", st.RewardSharePercent))
	}
}

func (d *dashboard) refreshPower(history []stats.Sample) {
	pkg, known := stats.MeanWatts(history, time.Minute)
	minerCPU, _ := stats.Mean(history, time.Minute, stats.MinerCPU)
	otherCPU, _ := stats.Mean(history, time.Minute, stats.OtherCPU)

	minerWatts, minerKnown := monitor.AttributeToMiner(pkg, minerCPU, otherCPU)
	if known && minerKnown {
		d.power.Set(formatWatts(minerWatts, true), "W")
	} else {
		d.power.Set(emDash, "")
	}
	d.powerDetail.Set("Whole package", func() string {
		if !known {
			return "counter not readable"
		}
		return formatWatts(pkg, true) + " W"
	}())

	hashrate, _ := stats.Mean(history, time.Minute, stats.Hashrate)
	d.powerDetail.Set("Efficiency", formatEfficiency(hashrate, minerWatts, known && minerKnown))

	if latest, ok := stats.Latest(history); ok {
		d.powerDetail.Set("CPU temperature", formatTemp(latest.TempC))
	}
}

func (d *dashboard) refreshConnection(cfg *config.Config) {
	p2pool := d.u.sup.P2Pool()
	if p2pool == nil {
		d.link.Set("Pool", "")
		d.link.SetColor(colorForeground)
		d.linkDetail.Set("Node", cfg.PoolURL)
		return
	}
	if p2pool.Ready() {
		d.link.Set("Connected", "")
		d.link.SetColor(colorOK)
	} else {
		d.link.Set("Syncing", "")
		d.link.SetColor(colorAccent)
	}

	addr, viaTor := d.u.sup.Node()
	if addr != "" {
		if viaTor {
			addr += " · via Tor"
		}
		d.linkDetail.Set("Node", addr)
	}
	d.linkDetail.Set("P2Pool chain", cfg.P2PoolChain)

	st, ok := p2pool.Stats()
	if !ok {
		return
	}
	d.linkDetail.Set("Shares found", fmt.Sprintf("%d", st.SharesFound))
	d.linkDetail.Set("You land a share", formatShareInterval(st.ShareInterval()))
	if occupancy, ok := st.WindowOccupancy(); ok {
		d.linkDetail.Set("Time in the payout window", formatPercent(occupancy))
	}
	d.linkDetail.Set("Next reward", rewardValue(st, true))
}

func (d *dashboard) refreshChart(history []stats.Sample, cfg *config.Config, preset kindness.Preset, paused bool) {
	d.chart.Set(history, preset.Ceiling, cfg.Chart)

	if latest, ok := stats.Latest(history); ok {
		setText(d.totalNow, formatPercent(clamp01(latest.Total()))+" total")
	}
	setText(d.backoff, "  ·  "+formatBackoff(stats.CountBackoffs(history, time.Minute), paused))
}

// setText updates a canvas.Text only when it changed, which keeps the
// once-a-second refresh from repainting the whole header.
func setText(t *canvas.Text, s string) {
	if t.Text == s {
		return
	}
	t.Text = s
	t.Refresh()
}

// formatCountdown renders the wait before full kindness, coarsening above 90
// seconds so the label is not restless.
func formatCountdown(secs int) string {
	if secs <= 90 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("~%dm", (secs+59)/60)
}

// formatBigNumber renders large counts (difficulty, hashrates) with a metric
// suffix rather than a wall of digits.
func formatBigNumber(v float64) string {
	switch {
	case v >= 1e12:
		return fmt.Sprintf("%.2f T", v/1e12)
	case v >= 1e9:
		return fmt.Sprintf("%.2f G", v/1e9)
	case v >= 1e6:
		return fmt.Sprintf("%.2f M", v/1e6)
	case v >= 1e3:
		return fmt.Sprintf("%.2f k", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}
