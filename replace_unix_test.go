//go:build unix

//nolint:testpackage // exercises unexported replaceFile
package selfupdate

import (
	"os"
	"strings"
	"syscall"
	"testing"
)

// TestReplaceFile_ModeIgnoresUmask pins the requested mode even when the
// process umask would strip group and other bits from a freshly created
// file. Package tests are not parallel, so the umask swap is safe.
func TestReplaceFile_ModeIgnoresUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	t.Cleanup(func() { syscall.Umask(old) })
	target := writeTarget(t, contentOld)
	if err := replaceFile(target, strings.NewReader(contentNew), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v, want 0755 regardless of umask", info.Mode().Perm())
	}
}
