package hotkey

import "testing"

func TestToAccelerator(t *testing.T) {
	for in, want := range map[string]string{
		"Ctrl+Shift+Q":             "Ctrl+Shift+Q",
		"Ctrl+Alt+D1":              "Ctrl+Alt+1",
		"Windows+F1":               "Win+F1",
		"Shift+OemTilde":           "Shift+`",
		"Ctrl+Oem3":                "Ctrl+`",
		"ctrl+shift+q":             "Ctrl+Shift+Q",
		"Control+F24":              "Ctrl+F24",
		"Alt+NumPad7":              "Alt+Num7",
		"Ctrl+Multiply":            "Ctrl+NumMult",
		"Ctrl+OemPlus":             "Ctrl+=",
		"Ctrl+OemMinus":            "Ctrl+-",
		"Ctrl+OemOpenBrackets":     "Ctrl+[",
		"Ctrl+OemPipe":             "Ctrl+\\",
		"Ctrl+OemQuestion":         "Ctrl+/",
		"Ctrl+OemQuotes":           "Ctrl+'",
		"Ctrl+OemSemicolon":        "Ctrl+;",
		"Ctrl+OemComma":            "Ctrl+,",
		"Ctrl+OemPeriod":           "Ctrl+.",
		"Ctrl+Alt+Left":            "Ctrl+Alt+Left",
		"Ctrl+PageDown":            "Ctrl+PageDown",
		"Ctrl+Next":                "Ctrl+PageDown",
		"Ctrl+Space":               "Ctrl+Space",
		"Ctrl+Return":              "Ctrl+Enter",
		" Ctrl + Shift + Q ":       "Ctrl+Shift+Q",
		"Shift+Alt+Ctrl+Windows+Z": "Ctrl+Alt+Shift+Win+Z",
	} {
		got, ok := ToAccelerator(in)
		if !ok || got != want {
			t.Errorf("ToAccelerator(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestToAcceleratorRejects(t *testing.T) {
	for _, in := range []string{"", "Q", "Ctrl+", "Ctrl+Shift+", "+Q", "Ctrl+Banana", "Hyper+Q", "Ctrl+Shift", "Ctrl+LeftShift", "Ctrl+F25", "Ctrl+D10", "garbage", "Ctrl+Oem102"} {
		if got, ok := ToAccelerator(in); ok || got != "" {
			t.Errorf("ToAccelerator(%q) = %q, %v; want rejected", in, got, ok)
		}
	}
}

func TestFromKeys(t *testing.T) {
	cases := []struct {
		mods []string
		key  string
		want string
	}{
		{[]string{"Shift", "Ctrl"}, "Q", "Ctrl+Shift+Q"},
		{[]string{"Alt", "Ctrl"}, "D1", "Ctrl+Alt+D1"},
		{[]string{"Windows"}, "F1", "Windows+F1"},
		{[]string{"Shift"}, "OemTilde", "Shift+OemTilde"},
		{[]string{"ctrl"}, "q", "Ctrl+Q"},
		{nil, "Q", ""},
		{[]string{"Ctrl"}, "", ""},
		{[]string{"Ctrl"}, "Banana", ""},
		{[]string{"Hyper"}, "Q", ""},
	}
	for _, c := range cases {
		if got := FromKeys(c.mods, c.key); got != c.want {
			t.Errorf("FromKeys(%v, %q) = %q; want %q", c.mods, c.key, got, c.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	for _, s := range []string{"Ctrl+Shift+Q", "Ctrl+Alt+D1", "Windows+F1", "Shift+OemTilde", "Alt+NumPad0"} {
		if _, ok := ToAccelerator(s); !ok {
			t.Errorf("%q", s)
		}
	}
}
