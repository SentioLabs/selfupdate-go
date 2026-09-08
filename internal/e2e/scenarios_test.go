package e2e_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// --- Contract assertions ---
// These pin the harness signatures the scenarios depend on.
var (
	_ func(*testing.T, string, bool) *releaseServer         = newReleaseServer
	_ func(*testing.T, string)                              = installOld
	_ func(*testing.T, string, string, ...string) runResult = run
	_ func(*testing.T, string) string                       = versionOf
	_ func(*testing.T)                                      = requireBinaries
)

const (
	preLine        = "pre-install: binary reports "
	postLine       = "post-install: binary reports "
	dlChecksumsTxt = "/dl/checksums.txt"
)

// assertNoLeftovers fails when a .new or .old file sits beside path.
func assertNoLeftovers(t *testing.T, path string) {
	t.Helper()
	for _, leftover := range []string{path + ".new", path + ".old"} {
		if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s must not exist after an update (stat err %v)", leftover, err)
		}
	}
}

// assertMode0755 fails when path is not executable by everyone.
func assertMode0755(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v, want 0755", info.Mode().Perm())
	}
}

// assertNoHookLines fails when either hook printed.
func assertNoHookLines(t *testing.T, stdout string) {
	t.Helper()
	if strings.Contains(stdout, preLine) || strings.Contains(stdout, postLine) {
		t.Fatalf("hooks must not run:\n%s", stdout)
	}
}

// failRun reports a subprocess result in full.
func failRun(t *testing.T, what string, res runResult) {
	t.Helper()
	t.Fatalf("%s: exit %d\nstdout:\n%s\nstderr:\n%s", what, res.Code, res.Stdout, res.Stderr)
}

// resolved returns path with symlinks resolved, matching what the
// installer prints.
func resolved(t *testing.T, path string) string {
	t.Helper()
	p, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// updateOld installs the old build at path, runs self update -y against
// a server advertising latest, and returns the result and the server.
func updateOld(t *testing.T, path, latest string, tamper bool) (runResult, *releaseServer) {
	t.Helper()
	s := newReleaseServer(t, latest, tamper)
	installOld(t, path)
	return run(t, path, s.URL(), "self", "update", "-y"), s
}

func TestUpdate_ReplacesRunningBinary(t *testing.T) {
	requireBinaries(t)
	bin := filepath.Join(t.TempDir(), "bin", "mytool")
	res, s := updateOld(t, bin, newVersion, false)
	if res.Code != 0 {
		failRun(t, "self update", res)
	}
	asset := assetName(newVersion)
	for _, want := range []string{
		"Downloading " + asset,
		"Verified " + asset,
		"Installed mytool " + newVersion + " to " + resolved(t, bin),
	} {
		if !strings.Contains(res.Stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, res.Stdout)
		}
	}
	if got := versionOf(t, bin); got != newVersion {
		t.Fatalf("binary on disk reports %q, want %q", got, newVersion)
	}
	assertMode0755(t, bin)
	assertNoLeftovers(t, bin)
	if got := s.dlRequests(); len(got) != 2 || got[0] != "/dl/"+asset || got[1] != dlChecksumsTxt {
		t.Fatalf("download requests: %v", got)
	}
}

func TestUpdate_HooksObserveTheSwap(t *testing.T) {
	requireBinaries(t)
	bin := filepath.Join(t.TempDir(), "bin", "mytool")
	res, _ := updateOld(t, bin, newVersion, false)
	if res.Code != 0 {
		failRun(t, "self update", res)
	}
	pre := strings.Index(res.Stdout, preLine+oldVersion)
	inst := strings.Index(res.Stdout, "Installed mytool")
	post := strings.Index(res.Stdout, postLine+newVersion)
	if pre < 0 || inst < 0 || post < 0 || pre > inst || inst > post {
		t.Fatalf("hook lines out of order or missing (pre=%d installed=%d post=%d):\n%s", pre, inst, post, res.Stdout)
	}
}

func TestCheckOnly_InstallsNothing(t *testing.T) {
	requireBinaries(t)
	s := newReleaseServer(t, newVersion, false)
	bin := filepath.Join(t.TempDir(), "bin", "mytool")
	installOld(t, bin)
	res := run(t, bin, s.URL(), "self", "update", "--check")
	if res.Code != 0 {
		failRun(t, "self update --check", res)
	}
	if !strings.Contains(res.Stdout, "Update available: "+oldVersion+" -> "+newVersion) {
		t.Fatalf("stdout:\n%s", res.Stdout)
	}
	if got := versionOf(t, bin); got != oldVersion {
		t.Fatalf("binary changed to %q", got)
	}
	assertNoHookLines(t, res.Stdout)
	if got := s.dlRequests(); len(got) != 0 {
		t.Fatalf("check must not download: %v", got)
	}
}

func TestUpToDate_InstallsNothing(t *testing.T) {
	requireBinaries(t)
	bin := filepath.Join(t.TempDir(), "bin", "mytool")
	res, s := updateOld(t, bin, oldVersion, false)
	if res.Code != 0 {
		failRun(t, "self update", res)
	}
	if !strings.Contains(res.Stdout, "is up to date") {
		t.Fatalf("stdout:\n%s", res.Stdout)
	}
	assertNoHookLines(t, res.Stdout)
	if got := s.dlRequests(); len(got) != 0 {
		t.Fatalf("up to date must not download: %v", got)
	}
	if got := versionOf(t, bin); got != oldVersion {
		t.Fatalf("binary changed to %q", got)
	}
}

func TestChecksumMismatch_LeavesBinary(t *testing.T) {
	requireBinaries(t)
	bin := filepath.Join(t.TempDir(), "bin", "mytool")
	res, _ := updateOld(t, bin, newVersion, true)
	if res.Code == 0 {
		failRun(t, "self update must fail on a bad checksum", res)
	}
	if !strings.Contains(res.Stderr, "checksum mismatch") {
		t.Fatalf("stderr:\n%s", res.Stderr)
	}
	if got := versionOf(t, bin); got != oldVersion {
		t.Fatalf("binary changed to %q", got)
	}
	assertNoHookLines(t, res.Stdout)
	assertNoLeftovers(t, bin)
}

func TestSymlinkedPath_ReplacesTarget(t *testing.T) {
	requireBinaries(t)
	root := t.TempDir()
	realPath := filepath.Join(root, "opt", "mytool", "bin", "mytool")
	link := filepath.Join(root, "bin", "mytool")
	s := newReleaseServer(t, newVersion, false)
	installOld(t, realPath)
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realPath, link); err != nil {
		t.Fatal(err)
	}
	res := run(t, link, s.URL(), "self", "update", "-y")
	if res.Code != 0 {
		failRun(t, "self update via symlink", res)
	}
	if got := versionOf(t, realPath); got != newVersion {
		t.Fatalf("real file reports %q", got)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink must survive; only its target is replaced")
	}
	if !strings.Contains(res.Stdout, preLine+oldVersion) || !strings.Contains(res.Stdout, postLine+newVersion) {
		t.Fatalf("hook lines missing:\n%s", res.Stdout)
	}
}

func TestManagedPath_Refused(t *testing.T) {
	requireBinaries(t)
	bin := filepath.Join(t.TempDir(), "Cellar", "mytool", "1.0.0", "bin", "mytool")
	res, s := updateOld(t, bin, newVersion, false)
	if res.Code == 0 {
		failRun(t, "self update must refuse a Cellar path", res)
	}
	for _, want := range []string{"Homebrew", "brew upgrade mytool"} {
		if !strings.Contains(res.Stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, res.Stderr)
		}
	}
	if got := s.dlRequests(); len(got) != 0 {
		t.Fatalf("managed refusal must happen before any download: %v", got)
	}
	if got := versionOf(t, bin); got != oldVersion {
		t.Fatalf("binary changed to %q", got)
	}
}

func TestUpdate_CodesignValidOnDarwin(t *testing.T) {
	requireBinaries(t)
	if runtime.GOOS != "darwin" {
		t.Skip("codesign is macOS only")
	}
	bin := filepath.Join(t.TempDir(), "bin", "mytool")
	res, _ := updateOld(t, bin, newVersion, false)
	if res.Code != 0 {
		failRun(t, "self update", res)
	}
	out, err := exec.CommandContext(t.Context(), "codesign", "--verify", "--verbose", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("codesign --verify failed: %v\n%s", err, out)
	}
}
