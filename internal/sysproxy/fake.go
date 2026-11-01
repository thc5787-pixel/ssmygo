package sysproxy

import "errors"

// Fake is an in-memory Proxy for tests. A *PartialError in SetErr is
// returned after applying the setting, as the Windows implementation does.
type Fake struct {
	Cur    Settings
	SetErr error
	Calls  []Settings
}

func (f *Fake) Get() (Settings, error) { return f.Cur, nil }

func (f *Fake) Set(s Settings) error {
	var pe *PartialError
	if f.SetErr != nil && !errors.As(f.SetErr, &pe) {
		return f.SetErr
	}
	f.Calls = append(f.Calls, s)
	f.Cur = s
	return f.SetErr
}
