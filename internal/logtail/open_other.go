//go:build !windows

package logtail

import "os"

// openShared opens path for reading; elsewhere renames never conflict.
func openShared(path string) (*os.File, error) { return os.Open(path) }
