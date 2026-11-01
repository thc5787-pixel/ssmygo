package plugin

import (
	"os"
	"testing"
	"time"
)

// exeTempDir is t.TempDir for directories holding freshly written .exe
// files. Windows (typically antivirus scanning a new executable) can hold
// such a file for seconds after its process exited; cleanup retries quietly
// instead of failing an otherwise passing test.
func exeTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "ssmygo-exe")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for i := 0; i < 40; i++ {
			if os.RemoveAll(dir) == nil {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Logf("left %s behind (still locked)", dir)
	})
	return dir
}
