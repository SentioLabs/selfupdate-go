package e2e_test

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Set by TestMain. Absolute paths of the two builds of examples/mytool.
var (
	oldBinary string // built with version v1.0.0
	newBinary string // built with version v1.1.0
)

const (
	oldVersion = "v1.0.0"
	newVersion = "v1.1.0"
	envAPI     = "MYTOOL_GITHUB_API"
)

// TestMain builds the demo twice. Under -short it builds nothing and every
// scenario skips itself through requireBinaries.
func TestMain(m *testing.M) {
	flag.Parse()
	if testing.Short() {
		os.Exit(m.Run())
	}
	dir, err := os.MkdirTemp("", "selfupdate-e2e-*")
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "e2e: temp dir:", err)
		os.Exit(1)
	}
	oldBinary, err = buildDemo(dir, oldVersion)
	if err == nil {
		newBinary, err = buildDemo(dir, newVersion)
	}
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "e2e:", err)
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// buildDemo compiles examples/mytool with version baked in and returns the
// binary path. The test's working directory is this package, so the
// relative source path resolves.
func buildDemo(dir, version string) (string, error) {
	out := filepath.Join(dir, "mytool-"+version)
	//nolint:gosec // version is one of the package's own oldVersion/newVersion constants, not external input
	cmd := exec.Command("go", "build", "-ldflags", "-X main.version="+version, "-o", out, "../../examples/mytool")
	if b, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build %s: %w\n%s", version, err, b)
	}
	return out, nil
}

// requireBinaries skips t under -short, when TestMain built nothing.
func requireBinaries(t *testing.T) {
	t.Helper()
	if oldBinary == "" {
		t.Skip("e2e suite skipped in -short mode")
	}
}
