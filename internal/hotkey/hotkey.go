// Package hotkey converts the original client's hotkey strings (WPF
// ModifierKeys and Key names joined by "+", as "Ctrl+Shift+Q" or
// "Ctrl+Alt+D1") to MyGo accelerators. It does not depend on MyGo.
package hotkey

import (
	"strconv"
	"strings"
)

// Modifier bits, in the order the original records them.
const (
	modCtrl = 1 << iota
	modAlt
	modShift
	modWin
)

var modNames = map[string]int{
	"ctrl": modCtrl, "control": modCtrl,
	"alt":     modAlt,
	"shift":   modShift,
	"windows": modWin, "win": modWin,
}

// keys maps a WPF Key name (lower case) to its canonical WPF name and its
// MyGo accelerator key.
var keys = map[string][2]string{}

func addKey(wpf, acc string, aliases ...string) {
	keys[strings.ToLower(wpf)] = [2]string{wpf, acc}
	for _, a := range aliases {
		keys[strings.ToLower(a)] = [2]string{wpf, acc}
	}
}

func init() {
	for c := 'A'; c <= 'Z'; c++ {
		addKey(string(c), string(c))
	}
	for d := 0; d <= 9; d++ {
		n := strconv.Itoa(d)
		addKey("D"+n, n, n)
		addKey("NumPad"+n, "Num"+n)
	}
	for f := 1; f <= 24; f++ {
		n := "F" + strconv.Itoa(f)
		addKey(n, n)
	}
	addKey("Multiply", "NumMult")
	addKey("Add", "NumAdd")
	addKey("Subtract", "NumSub")
	addKey("Decimal", "NumDec")
	addKey("Divide", "NumDiv")
	addKey("OemTilde", "`", "Oem3")
	addKey("OemMinus", "-")
	addKey("OemPlus", "=")
	addKey("OemOpenBrackets", "[", "Oem4")
	addKey("OemCloseBrackets", "]", "Oem6")
	addKey("OemPipe", "\\", "Oem5")
	addKey("OemSemicolon", ";", "Oem1")
	addKey("OemQuotes", "'", "Oem7")
	addKey("OemComma", ",")
	addKey("OemPeriod", ".")
	addKey("OemQuestion", "/", "Oem2")
	addKey("Left", "Left")
	addKey("Up", "Up")
	addKey("Right", "Right")
	addKey("Down", "Down")
	addKey("Home", "Home")
	addKey("End", "End")
	addKey("PageUp", "PageUp", "Prior")
	addKey("PageDown", "PageDown", "Next")
	addKey("Insert", "Insert")
	addKey("Delete", "Delete")
	addKey("Space", "Space")
	addKey("Tab", "Tab")
	addKey("Enter", "Enter", "Return")
	addKey("Escape", "Escape")
}

// parse splits an original string into modifier bits and the key's entry.
// At least one modifier is required, as the original's Str2HotKey does.
func parse(s string) (int, [2]string, bool) {
	i := strings.LastIndex(s, "+")
	if i <= 0 {
		return 0, [2]string{}, false
	}
	k, ok := keys[strings.ToLower(strings.TrimSpace(s[i+1:]))]
	if !ok {
		return 0, [2]string{}, false
	}
	mods := 0
	for _, m := range strings.Split(s[:i], "+") {
		bit, ok := modNames[strings.ToLower(strings.TrimSpace(m))]
		if !ok {
			return 0, [2]string{}, false
		}
		mods |= bit
	}
	return mods, k, true
}

func join(mods int, names [4]string, key string) string {
	var parts []string
	for i, n := range names {
		if mods&(1<<i) != 0 {
			parts = append(parts, n)
		}
	}
	return strings.Join(append(parts, key), "+")
}

// ToAccelerator converts an original hotkey string to a MyGo accelerator.
// It returns "", false for an empty string or one it cannot convert.
func ToAccelerator(original string) (string, bool) {
	mods, k, ok := parse(original)
	if !ok {
		return "", false
	}
	return join(mods, [4]string{"Ctrl", "Alt", "Shift", "Win"}, k[1]), true
}

// FromKeys returns the original string of a recorded combination: mods are
// "Ctrl", "Alt", "Shift" or "Windows", key a WPF Key name such as "Q",
// "D1" or "OemTilde". It returns "" without a modifier or for a key it
// does not know.
func FromKeys(mods []string, key string) string {
	k, ok := keys[strings.ToLower(key)]
	if !ok || len(mods) == 0 {
		return ""
	}
	bits := 0
	for _, m := range mods {
		bit, ok := modNames[strings.ToLower(m)]
		if !ok {
			return ""
		}
		bits |= bit
	}
	return join(bits, [4]string{"Ctrl", "Alt", "Shift", "Windows"}, k[0])
}
