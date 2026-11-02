package ui

import (
	"errors"
	"image"
	"strings"
	"testing"

	"github.com/thc5787/ssmygo/internal/config"
)

type scanRec struct {
	imported  string
	asked     string
	added     string
	updated   string
	opened    bool
	notes     []string
	answerYes bool
}

func scanWith(r *scanRec, texts []string, captureErr error) {
	scanScreen(scanDeps{
		capture: func() ([]image.Image, error) {
			if captureErr != nil {
				return nil, captureErr
			}
			return []image.Image{image.NewRGBA(image.Rect(0, 0, 1, 1))}, nil
		},
		decode: func(image.Image) []string { return texts },
		importURLs: func(s string) ([]config.Server, []error) {
			r.imported = s
			servers, errs := config.ParseURLs(s)
			return servers, errs
		},
		askSubscribe:       func(u string) bool { r.asked = u; return r.answerYes },
		addSubscription:    func(u string) error { r.added = u; return nil },
		updateSubscription: func(u string) { r.updated = u },
		openEditor:         func() { r.opened = true },
		notify:             func(s string) { r.notes = append(r.notes, s) },
	})
}

const goodSS = "ss://YWVzLTI1Ni1nY206cGFzc3dvcmQ@ss.example.com:8388#Tokyo"

func TestScanImportsSS(t *testing.T) {
	var r scanRec
	scanWith(&r, []string{goodSS}, nil)
	if r.imported != goodSS || !r.opened || r.asked != "" {
		t.Fatalf("%+v", r)
	}
}

// C-D2: an http(s) link is offered as a subscription, never opened.
func TestScanOffersSubscription(t *testing.T) {
	r := scanRec{answerYes: true}
	scanWith(&r, []string{"https://sub.example.com/s?token=x"}, nil)
	if r.asked != "https://sub.example.com/s?token=x" || r.added != r.asked || r.updated != r.asked {
		t.Fatalf("%+v", r)
	}
	r2 := scanRec{answerYes: false}
	scanWith(&r2, []string{"https://sub.example.com/s"}, nil)
	if r2.added != "" || r2.updated != "" {
		t.Fatalf("added without consent: %+v", r2)
	}
}

func TestScanNone(t *testing.T) {
	var r scanRec
	scanWith(&r, nil, nil)
	if len(r.notes) != 1 || !strings.Contains(r.notes[0], "No QRCode found") {
		t.Fatalf("%q", r.notes)
	}
}

// Review Focus 4: an invalid ss:// code never shows its secret.
func TestScanInvalidMasksPassword(t *testing.T) {
	var r scanRec
	scanWith(&r, []string{"ss://bWV0aG9kOnNlY3JldA@"}, nil) // "method:secret", no host
	if len(r.notes) != 1 || !strings.Contains(r.notes[0], "Invalid QR Code content") ||
		strings.Contains(r.notes[0], "bWV0aG9kOnNlY3JldA") || strings.Contains(r.notes[0], "secret") {
		t.Fatalf("%q", r.notes)
	}
	var r2 scanRec
	scanWith(&r2, []string{"hello world"}, nil)
	if !strings.Contains(r2.notes[0], "hello world") {
		t.Fatalf("plain text not shown: %q", r2.notes)
	}
}

func TestScanCaptureError(t *testing.T) {
	var r scanRec
	scanWith(&r, nil, errors.New("no desktop"))
	if len(r.notes) != 1 || !strings.Contains(r.notes[0], "Screen capture failed") {
		t.Fatalf("%q", r.notes)
	}
}
