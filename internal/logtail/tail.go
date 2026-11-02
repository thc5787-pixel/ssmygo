// Package logtail follows ssmygo.log for the log window, like tail -f.
package logtail

import (
	"bytes"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

const (
	window  = 64 << 10 // first read, and the most read per Poll
	maxLine = 4096     // display cut, bytes
)

// Tail follows one file path across rotation (rename) and truncation.
type Tail struct {
	path    string
	info    os.FileInfo // last seen; nil before the first successful Poll
	offset  int64
	pending []byte // a line not yet ended by '\n'

	// Rotated is where the log moves when it rotates (logx: ssmygo.1.log).
	// When set, a rotation finishes the old file instead of starting over.
	Rotated string
}

// Open starts following path. Nothing is read until Poll.
func Open(path string) *Tail { return &Tail{path: path} }

// Poll returns the complete lines appended since the last call. The first
// call, and any call that finds more than 64 KB new, starts 64 KB before
// the end at the next line start. reset reports that the file was rotated,
// replaced or truncated: the caller should clear what it shows. err is
// non-nil only when the file is not available; the next Poll retries.
//
// The file is opened and closed on every call, so it never blocks logx's
// rename on Windows.
func (t *Tail) Poll() (lines []string, reset bool, err error) {
	f, err := openShared(t.path)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}
	switch {
	case t.info != nil && !os.SameFile(t.info, info):
		// Rotated: finish the old file under its new name, then read the
		// new one from its start. Without the old file, start over.
		old, ok := t.finishRotated()
		if !ok {
			reset = true
		}
		lines = old
		t.offset, t.pending = 0, nil
	case t.info != nil && info.Size() < t.offset: // truncated
		reset = true
		t.offset, t.pending = 0, nil
	}
	first := t.info == nil
	t.info = info
	start := t.offset
	skipPartial := false
	if size := info.Size(); size-start > window || (first && size > window) {
		start, skipPartial, t.pending = size-window, true, nil
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, reset, err
	}
	b, err := io.ReadAll(io.LimitReader(f, info.Size()-start))
	if err != nil {
		return nil, reset, err
	}
	t.offset = start + int64(len(b))
	if skipPartial {
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			b = b[i+1:]
		} else {
			b = nil
		}
	}
	got, rest := splitLines(append(t.pending, b...))
	lines = append(lines, got...)
	t.pending = nil
	if len(rest) > window { // a runaway line with no end: show what we have
		lines = append(lines, string(rest))
	} else if len(rest) > 0 {
		t.pending = append([]byte(nil), rest...)
	}
	return lines, reset, nil
}

// finishRotated reads what the old file gained since the last Poll, from
// t.Rotated, if that is the file this Tail was reading. Its last line is
// complete even without a newline: nothing more will be written to it.
func (t *Tail) finishRotated() ([]string, bool) {
	if t.Rotated == "" {
		return nil, false
	}
	f, err := openShared(t.Rotated)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !os.SameFile(t.info, info) || info.Size() < t.offset {
		return nil, false
	}
	start := max(t.offset, info.Size()-window)
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return nil, false
	}
	b, err := io.ReadAll(io.LimitReader(f, info.Size()-start))
	if err != nil {
		return nil, false
	}
	lines, rest := splitLines(append(t.pending, b...))
	if len(rest) > 0 {
		lines = append(lines, string(rest))
	}
	return lines, true
}

// splitLines cuts data at '\n' (dropping a '\r' before it) and returns the
// complete lines and what follows the last newline.
func splitLines(data []byte) (lines []string, rest []byte) {
	for {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return lines, data
		}
		lines = append(lines, string(bytes.TrimSuffix(data[:i], []byte{'\r'})))
		data = data[i+1:]
	}
}

// CutLine prepares a line for display: invalid UTF-8 becomes U+FFFD and
// anything over 4096 bytes is cut at a rune boundary with "…".
func CutLine(s string) string {
	if !utf8.ValidString(s) {
		var b strings.Builder
		for i := 0; i < len(s); {
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				b.WriteString("�") // one per bad byte
			} else {
				b.WriteString(s[i : i+n])
			}
			i += n
		}
		s = b.String()
	}
	if len(s) <= maxLine {
		return s
	}
	cut := maxLine
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// IsPluginOutput reports whether line is a plugin's output (logged by the
// plugin supervisor as msg="plugin output").
func IsPluginOutput(line string) bool { return strings.Contains(line, ` msg="plugin output"`) }

// Buffer keeps the newest lines for display.
type Buffer struct {
	max   int
	lines []string
}

// NewBuffer keeps at most max lines.
func NewBuffer(max int) *Buffer { return &Buffer{max: max} }

// Append adds lines, dropping the oldest beyond the limit.
func (b *Buffer) Append(lines ...string) {
	b.lines = append(b.lines, lines...)
	if over := len(b.lines) - b.max; over > 0 {
		b.lines = append(b.lines[:0], b.lines[over:]...)
	}
}

// Clear empties the buffer (the file is untouched).
func (b *Buffer) Clear() { b.lines = b.lines[:0] }

// Lines returns the kept lines, without plugin output when filterPlugin.
func (b *Buffer) Lines(filterPlugin bool) []string {
	out := make([]string, 0, len(b.lines))
	for _, l := range b.lines {
		if filterPlugin && IsPluginOutput(l) {
			continue
		}
		out = append(out, l)
	}
	return out
}
