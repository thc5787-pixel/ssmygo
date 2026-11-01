//go:build !windows

package sysproxy

type unsupported struct{}

func (unsupported) Get() (Settings, error) { return Settings{}, ErrUnsupported }
func (unsupported) Set(Settings) error     { return ErrUnsupported }

// Windows returns the platform proxy; off Windows it always fails.
func Windows() Proxy { return unsupported{} }
