package sysproxy

// Fake is an in-memory Proxy for tests.
type Fake struct {
	Cur    Settings
	SetErr error
	Calls  []Settings
}

func (f *Fake) Get() (Settings, error) { return f.Cur, nil }

func (f *Fake) Set(s Settings) error {
	if f.SetErr != nil {
		return f.SetErr
	}
	f.Calls = append(f.Calls, s)
	f.Cur = s
	return nil
}
