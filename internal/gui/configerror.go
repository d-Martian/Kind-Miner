package gui

import (
	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/config"
)

// RunConfigError shows a standalone window for an unreadable or invalid
// configuration and blocks until dismissed. It is used when the GUI cannot
// even begin startup because the existing config can't be loaded.
func RunConfigError(err error) {
	a := fyneapp.NewWithID(appID)
	a.Settings().SetTheme(kindTheme{})
	currentApp = a

	w := a.NewWindow("kind-miner — configuration problem")
	w.Resize(fyne.NewSize(480, 240))
	w.CenterOnScreen()

	title := canvas.NewText("Configuration problem", colorError)
	title.TextSize = 20
	title.TextStyle = fyne.TextStyle{Bold: true}

	msg := widget.NewLabel(err.Error())
	msg.Wrapping = fyne.TextWrapWord

	openCfg := widget.NewButton("Open config", func() { openInEditor(config.Path()) })
	quit := widget.NewButton("Quit", func() { a.Quit() })
	buttons := container.NewHBox(openCfg, widget.NewLabel(""), quit)

	w.SetContent(container.NewBorder(nil, buttons, nil, nil,
		container.NewPadded(container.NewVBox(title, msg))))
	w.ShowAndRun()
}
