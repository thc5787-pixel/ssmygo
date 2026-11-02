// Package geosite downloads and verifies v2fly's dlc.dat (the original
// client's "Update Local PAC from Geosite"). It does not depend on MyGo.
package geosite

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/thc5787/ssmygo/internal/rules"
)

const (
	DefaultURL    = "https://github.com/v2fly/domain-list-community/raw/release/dlc.dat"
	DefaultSumURL = DefaultURL + ".sha256sum"
	// MaxBytes limits the download, counted after decompression.
	MaxBytes = 20 << 20
	// Timeout is the whole update's time limit.
	Timeout = 60 * time.Second
	maxSum  = 4 << 10
)

var (
	// ErrFetch wraps download failures. Error texts name the host only.
	ErrFetch    = errors.New("geosite download failed")
	ErrChecksum = errors.New("geosite checksum mismatch")
	ErrTooLarge = errors.New("geosite download larger than 20 MB")
	ErrNotDLC   = errors.New("downloaded file is not a geosite dlc.dat")
)

// MissingGroupsError: the download lacks groups the configuration uses.
type MissingGroupsError struct{ Groups []string }

func (e *MissingGroupsError) Error() string {
	return "downloaded geosite lacks groups: " + strings.Join(e.Groups, ", ")
}

// Sources applies the original's rules to gui-config.json's geositeUrl and
// geositeSha256sumUrl: an empty URL means the default source, which is
// always verified; a custom source is verified only when it has a sum URL.
func Sources(rawURL, sumURL string) (string, string) {
	if strings.TrimSpace(rawURL) == "" {
		return DefaultURL, DefaultSumURL
	}
	return strings.TrimSpace(rawURL), strings.TrimSpace(sumURL)
}

// Fetch downloads dlc.dat (see Sources for rawURL and sumURL), verifies it
// against the sha256sum when there is one, and checks that it parses and
// has every group (name or name@attr) in groups.
func Fetch(ctx context.Context, rawURL, sumURL string, client *http.Client, groups []string) ([]byte, error) {
	rawURL, sumURL = Sources(rawURL, sumURL)
	var want string
	if sumURL != "" {
		b, err := get(ctx, client, sumURL, maxSum)
		if err != nil {
			return nil, err
		}
		f := strings.Fields(string(b))
		if len(f) == 0 {
			return nil, fmt.Errorf("%w: %s: empty sha256sum", ErrFetch, Host(sumURL))
		}
		want = strings.ToLower(f[0])
		if _, err := hex.DecodeString(want); err != nil || len(want) != 2*sha256.Size {
			return nil, fmt.Errorf("%w: %s: malformed sha256sum", ErrFetch, Host(sumURL))
		}
	}
	data, err := get(ctx, client, rawURL, MaxBytes)
	if err != nil {
		return nil, err
	}
	if want != "" && Sum(data) != want {
		return nil, fmt.Errorf("%w: %s", ErrChecksum, Host(rawURL))
	}
	list, err := rules.ParseDLC(data)
	if err != nil || len(list) == 0 {
		return nil, ErrNotDLC
	}
	if _, missing := rules.Select(list, groups); len(missing) > 0 {
		return nil, &MissingGroupsError{Groups: missing}
	}
	return data, nil
}

// Sum is data's sha256 in lowercase hex.
func Sum(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func get(ctx context.Context, client *http.Client, rawURL string, limit int64) ([]byte, error) {
	fail := func(format string, a ...any) error {
		return fmt.Errorf("%w: %s: %s", ErrFetch, Host(rawURL), fmt.Sprintf(format, a...))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fail("invalid URL")
	}
	resp, err := client.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the URL from the message
		}
		return nil, fail("%v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fail("HTTP %d", resp.StatusCode)
	}
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(resp.Body, limit+1))
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fail("%v", err)
	}
	if n > limit {
		if limit == MaxBytes {
			return nil, fmt.Errorf("%w: %s", ErrTooLarge, Host(rawURL))
		}
		return nil, fail("response too large")
	}
	return buf.Bytes(), nil
}

// Host is the URL's host name, for logs and messages.
func Host(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Hostname() == "" {
		return "(invalid URL)"
	}
	return u.Hostname()
}
