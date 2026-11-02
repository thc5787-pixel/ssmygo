package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"

	"github.com/thc5787/ssmygo/internal/config"
	"github.com/thc5787/ssmygo/internal/controller"
	"github.com/thc5787/ssmygo/internal/i18n"
	"github.com/thc5787/ssmygo/internal/qrcode"
	"github.com/thc5787/ssmygo/internal/screencap"
)

// scanDeps are the pieces of Scan QRCode from Screen, injectable for tests.
type scanDeps struct {
	capture            func() ([]image.Image, error)
	decode             func(image.Image) []string
	importURLs         func(string) ([]config.Server, []error)
	askSubscribe       func(url string) bool
	addSubscription    func(url string) error
	updateSubscription func(url string)
	openEditor         func(added []config.Server)
	notify             func(string)
}

func hasPrefixFold(s, p string) bool { return len(s) >= len(p) && strings.EqualFold(s[:len(p)], p) }

// scanScreen captures every display and acts on the QR codes found (spec
// §2.2): ss:// links are imported, an http(s) link is offered as a
// subscription (never opened, C-D2), anything else is reported.
func scanScreen(d scanDeps) {
	imgs, err := d.capture()
	if err != nil {
		d.notify(fmt.Sprintf(i18n.T("ssmygo.captureFailed"), err))
		return
	}
	var texts, links []string
	seen := map[string]bool{}
	for _, img := range imgs {
		for _, t := range d.decode(img) {
			if !seen[t] {
				seen[t] = true
				texts = append(texts, t)
				if hasPrefixFold(t, "ss://") {
					links = append(links, t)
				}
			}
		}
	}
	if len(texts) == 0 {
		d.notify(i18n.T("No QRCode found. Try to zoom in or move it to the center of the screen."))
		return
	}
	if len(links) > 0 {
		added, errs := d.importURLs(strings.Join(links, "\n"))
		if len(added) == 0 {
			d.notify(invalidQR(links[0]))
			return
		}
		d.openEditor(added)
		d.notify(fmt.Sprintf(i18n.T("ssmygo.imported"), len(added), len(errs)))
		return
	}
	for _, t := range texts {
		if hasPrefixFold(t, "http://") || hasPrefixFold(t, "https://") {
			if d.askSubscribe(t) {
				if err := d.addSubscription(t); err != nil && !errors.Is(err, controller.ErrDuplicateSubscription) {
					d.notify(Message(err))
					return
				}
				d.updateSubscription(t)
			}
			return
		}
	}
	d.notify(invalidQR(texts[0]))
}

// invalidQR is the original's "Invalid QR Code content" message. An ss://
// link that did not parse may hold a password, so only its scheme shows.
func invalidQR(text string) string {
	shown := text
	if hasPrefixFold(text, "ss://") {
		shown = "ss://…"
	} else if r := []rune(text); len(r) > 100 {
		shown = string(r[:100]) + "…"
	}
	return strings.Replace(i18n.T("Invalid QR Code content: {0}"), "{0}", shown, 1)
}

// realScanDeps wires the scan to the screen, the controller and the UI.
// It runs off the main thread (capture and the dialog block).
func realScanDeps(ctrl *controller.Controller, notify func(string), openEditor func([]config.Server)) scanDeps {
	return scanDeps{
		capture:    screencap.CaptureAll,
		decode:     qrcode.Decode,
		importURLs: ctrl.ImportURLs,
		askSubscribe: func(u string) bool {
			return confirm(i18n.T("ssmygo.addSubTitle"), u, i18n.T("ssmygo.addSubOK"))
		},
		addSubscription: ctrl.AddSubscription,
		updateSubscription: func(u string) {
			if _, err := ctrl.UpdateSubscription(context.Background(), u); err != nil {
				notify(Message(err))
			}
		},
		openEditor: openEditor,
		notify:     notify,
	}
}
