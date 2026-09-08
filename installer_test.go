//nolint:testpackage // needs the unexported ScriptInstaller.command method
package selfupdate

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// --- Contract assertions ---
var (
	_ Installer = (*ScriptInstaller)(nil)
	_ Staged    = (*scriptStaged)(nil)
)

// installScript prepares and commits tag through s, mirroring Updater.Update.
func installScript(ctx context.Context, s *ScriptInstaller, tag string) error {
	staged, err := s.Prepare(ctx, Release{Tag: tag})
	if err != nil {
		return err
	}
	defer func() { _ = staged.Close() }()
	return staged.Commit(ctx)
}

// requireCurlAndBash skips the test when either tool is missing from PATH.
func requireCurlAndBash(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("curl"); err != nil {
		t.Skip("curl not on PATH")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not on PATH")
	}
}

func TestScriptInstaller_Command(t *testing.T) {
	s := &ScriptInstaller{ScriptURL: "https://example.com/install.sh"}
	got, err := s.command("v1.2.3-rc.1")
	if err != nil {
		t.Fatal(err)
	}
	want := "curl -fsSL https://example.com/install.sh | bash -s -- --force --tag=v1.2.3-rc.1"
	if got != want {
		t.Fatalf("got %q", got)
	}
}

func TestScriptInstaller_RejectsUnsafeTag(t *testing.T) {
	s := &ScriptInstaller{ScriptURL: "https://example.com/install.sh"}
	for _, bad := range []string{"", "v1; rm -rf /", "v1 $(x)", "v1`x`", "v1&&x", "v1|x", "v1 x", "../x"} {
		if _, err := s.command(bad); err == nil {
			t.Errorf("tag %q must be rejected", bad)
		}
		if err := installScript(context.Background(), s, bad); err == nil {
			t.Errorf("Prepare with tag %q must fail before running anything", bad)
		}
	}
}

func TestScriptInstaller_RunsScriptViaFileURL(t *testing.T) {
	requireCurlAndBash(t)
	// curl accepts file:// URLs, so a local script stands in for the remote one.
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	body := "#!/usr/bin/env bash\necho \"args: $*\"\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // script must be executable
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := &ScriptInstaller{ScriptURL: "file://" + script, Stdout: &out, Stderr: &out}
	if err := installScript(context.Background(), s, "v9.9.9"); err != nil {
		t.Fatalf("install: %v, output: %s", err, out.String())
	}
	if !strings.Contains(out.String(), "args: --force --tag=v9.9.9") {
		t.Fatalf("script did not receive the expected arguments: %s", out.String())
	}
}

func TestScriptInstaller_CurlFailureIsError(t *testing.T) {
	requireCurlAndBash(t)
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.sh")
	var out bytes.Buffer
	s := &ScriptInstaller{ScriptURL: "file://" + missing, Stdout: &out, Stderr: &out}
	if err := installScript(context.Background(), s, "v1.0.0"); err == nil {
		t.Fatalf("expected error when curl fails, got nil, output: %s", out.String())
	}
}

func TestScriptInstaller_CancelStopsPipeline(t *testing.T) {
	requireCurlAndBash(t)
	if runtime.GOOS == "windows" {
		t.Skip("process groups are not used on windows")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	body := "#!/usr/bin/env bash\nsleep 31.415\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // script must be executable
		t.Fatal(err)
	}
	var out bytes.Buffer
	// A non-file Stdin keeps this on the detached-process-group path
	// regardless of what the test runner's own stdin happens to be.
	s := &ScriptInstaller{ScriptURL: "file://" + script, Stdout: &out, Stderr: &out, Stdin: strings.NewReader("")}
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- installScript(ctx, s, "v1.0.0")
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected an error from a cancelled install")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Install did not return within 3 seconds of cancellation")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		findErr := exec.Command("pgrep", "-f", "sleep 31.415").Run()
		if findErr != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sleep 31.415 is still running after cancellation")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestStdinIsTerminal(t *testing.T) {
	if stdinIsTerminal(nil) {
		t.Error("nil reader must not be a terminal")
	}
	if stdinIsTerminal(strings.NewReader("input")) {
		t.Error("a strings.Reader must not be a terminal")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	defer func() { _ = w.Close() }()
	if stdinIsTerminal(r) {
		t.Error("the read end of an os.Pipe must not be a terminal")
	}
}

func TestScriptInstaller_NonZeroExitIsError(t *testing.T) {
	requireCurlAndBash(t)
	dir := t.TempDir()
	script := filepath.Join(dir, "install.sh")
	body := "#!/usr/bin/env bash\nexit 3\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // script must be executable
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := &ScriptInstaller{ScriptURL: "file://" + script, Stdout: &out, Stderr: &out}
	if err := installScript(context.Background(), s, "v1.0.0"); err == nil {
		t.Fatal("expected error from exit 3")
	}
}

func TestScriptInstaller_PrepareRunsNothing(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "ran")
	script := filepath.Join(dir, "install.sh")
	body := "#!/usr/bin/env bash\ntouch " + marker + "\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil { //nolint:gosec // script must be executable
		t.Fatal(err)
	}
	var out bytes.Buffer
	s := &ScriptInstaller{ScriptURL: "file://" + script, Stdout: &out, Stderr: &out}
	staged, err := s.Prepare(context.Background(), Release{Tag: tagV100})
	if err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatal("Prepare must not run the script")
	}
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
}
