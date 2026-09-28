package gui

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/hub"
)

// The desktop is the Nodo's UI: Find a hub searches the LAN, and a hub with
// no config yet is set up from here — to pay the address this desktop
// already has — without anyone opening an SSH session. The code it gets back
// fills the pairing field, and the owner token it gets is what lets this
// desktop change the hub's wallet later.

// findHub opens the search.
func (f *settingsForm) findHub(parent fyne.Window) {
	status := widget.NewLabel(findHubSearching)
	status.Wrapping = fyne.TextWrapWord
	list := container.NewVBox()
	manual := widget.NewEntry()
	manual.SetPlaceHolder(findHubManual)

	var d dialog.Dialog
	show := func(found []hub.Found) {
		list.RemoveAll()
		for _, h := range found {
			list.Add(f.hubRow(h, parent, status, func() { d.Hide() }))
		}
		list.Refresh()
	}
	look := widget.NewButton(findHubLook, nil)
	look.OnTapped = func() {
		addr := hubAPIAddr(manual.Text)
		if addr == "" {
			return
		}
		look.Disable()
		go func() {
			defer look.Enable()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			hello, fp, err := hub.Probe(ctx, addr, [32]byte{})
			if err != nil {
				status.SetText(fmt.Sprintf("Nothing answered as a hub at %s: %v", addr, err))
				return
			}
			host, port, _ := net.SplitHostPort(addr)
			n, _ := strconv.Atoi(port)
			status.SetText(findHubSome)
			show([]hub.Found{{Hello: hello, Host: host, APIPort: n, Fingerprint: fp}})
		}()
	}

	content := container.NewVBox(status, list, container.NewBorder(nil, nil, nil, look, manual))
	d = dialog.NewCustom(findHubTitle, "Close", content, parent)
	d.Resize(fyne.NewSize(560, 320))
	d.Show()

	go func() {
		found, err := hub.Discover(context.Background())
		switch {
		case err != nil:
			status.SetText(fmt.Sprintf("Could not search this network (%v). Give the hub's address instead:", err))
		case len(found) == 0:
			status.SetText(findHubNone)
		default:
			status.SetText(findHubSome)
		}
		show(found)
	}()
}

// hubRow is one hub in the list: what it is, and the way to set it up if
// nobody has.
func (f *settingsForm) hubRow(h hub.Found, parent fyne.Window, status *widget.Label, done func()) fyne.CanvasObject {
	label := widget.NewLabel(hubRowText(h))
	label.Wrapping = fyne.TextWrapWord
	if h.SetUp {
		return label
	}
	btn := widget.NewButton(findHubSetUp, func() { f.confirmSetUp(h, parent, status, done) })
	return container.NewBorder(nil, nil, nil, btn, label)
}

// hubRowText describes a hub the search found.
func hubRowText(h hub.Found) string {
	what := h.Name
	if h.Nodo {
		what += " (Nodo)"
	}
	state := findHubNotSet
	if h.SetUp {
		state = findHubIsSetUp
	}
	return fmt.Sprintf("%s at %s — %s", what, h.Host, state)
}

// hubAPIAddr is the API address for what the user typed: a host alone gets
// the default API port.
func hubAPIAddr(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, _, err := net.SplitHostPort(s); err == nil {
		return s
	}
	return net.JoinHostPort(strings.Trim(s, "[]"), strconv.Itoa(hub.DefaultAPIPort))
}

// shortFingerprint is enough of a fingerprint to compare by eye with what
// kind-minerd pair prints on the hub.
func shortFingerprint(fp [32]byte) string {
	h := fmt.Sprintf("%x", fp[:8])
	return h[:4] + " " + h[4:8] + " " + h[8:12] + " " + h[12:]
}

// confirmSetUp asks before setting a hub up, since the address it names is
// the one every device in the house will then pay.
func (f *settingsForm) confirmSetUp(h hub.Found, parent fyne.Window, status *widget.Label, done func()) {
	address := strings.TrimSpace(f.wallet.Text)
	if ValidateAddress(address) != "" {
		dialog.ShowInformation(findHubTitle, findHubNeedsAddress, parent)
		return
	}
	msg := fmt.Sprintf(findHubConfirm, h.Name, shortenMiddle(address, 24), shortFingerprint(h.Fingerprint))
	c := dialog.NewConfirm(findHubTitle, msg, func(ok bool) {
		if !ok {
			return
		}
		status.SetText(fmt.Sprintf(findHubSettingUp, h.Name))
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			p, owner, err := hub.SetUp(ctx, h.APIAddr(), h.Fingerprint, address)
			if err != nil {
				status.SetText(fmt.Sprintf(findHubFailed, h.Name, err))
				return
			}
			f.pairedBySetUp(p.Code(), owner)
			f.hubNote.SetText(fmt.Sprintf(findHubDone, h.Name))
			done()
		}()
	}, parent)
	c.SetConfirmText("Set it up")
	c.SetDismissText("Cancel")
	c.Show()
}

// pairedBySetUp fills the form in for a hub this desktop just set up.
func (f *settingsForm) pairedBySetUp(code, owner string) {
	f.hubCode.SetText(code)
	f.mode.SetSelected(string(config.ModeHub))
	f.ownerToken, f.ownerCode = owner, code
}

// ownerTokenFor is the owner token to keep for the pairing code the form now
// holds: the one this desktop got for that hub, and none for any other. A
// token kept against a different hub would be sent to it on the next wallet
// change — refused, but sent. Hubs are told apart by certificate, not by
// code: pairing again after turning the onion on gives the same hub a new
// code, and its owner is still its owner.
func (f *settingsForm) ownerTokenFor(code string) string {
	if f.ownerToken == "" {
		return ""
	}
	now, err1 := hub.ParseCode(code)
	was, err2 := hub.ParseCode(f.ownerCode)
	if err1 != nil || err2 != nil || now.Fingerprint != was.Fingerprint {
		return ""
	}
	return f.ownerToken
}

// pushWallet sends a changed wallet to the hub this desktop owns. It runs on
// Save, before the config is written, so a hub that refused leaves the form
// unsaved with the reason shown rather than a desktop and hub that disagree.
func pushWallet(code, owner, wallet string) error {
	p, err := hub.ParseCode(code)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return hub.SetWallet(ctx, p, owner, wallet)
}
