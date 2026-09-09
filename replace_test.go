//nolint:testpackage // exercises unexported helpers and swaps renameFile
package selfupdate

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Fixture contents for the binary before and after an update.
const (
	contentOld = "old"
	contentNew = "new"
)

// writeTarget writes an executable fixture named tool in a temp dir.
func writeTarget(t *testing.T, content string) string {
	t.Helper()
	target := filepath.Join(t.TempDir(), "tool")
	if err := os.WriteFile(target, []byte(content), 0o755); err != nil { //nolint:gosec // executable fixture
		t.Fatal(err)
	}
	return target
}

// swapRename replaces renameFile for the test and restores it afterwards.
func swapRename(t *testing.T, fn func(from, to string) error) {
	t.Helper()
	orig := renameFile
	renameFile = fn
	t.Cleanup(func() { renameFile = orig })
}

func assertNoLeftovers(t *testing.T, target string) {
	t.Helper()
	for _, leftover := range []string{target + ".new", target + ".old"} {
		if _, err := os.Stat(leftover); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s must not exist after replaceFile (stat err %v)", leftover, err)
		}
	}
}

func readTarget(t *testing.T, target string) string {
	t.Helper()
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func TestReplaceFile_ReplacesAndCleansUp(t *testing.T) {
	target := writeTarget(t, contentOld)
	if err := replaceFile(target, strings.NewReader(contentNew), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != contentNew {
		t.Fatalf("content %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v, want 0755", info.Mode().Perm())
	}
	assertNoLeftovers(t, target)
}

func TestReplaceFile_FinalRenameFailureRestoresOriginal(t *testing.T) {
	target := writeTarget(t, contentOld)
	swapRename(t, func(from, to string) error {
		if from == target+".new" {
			return errors.New("simulated rename failure")
		}
		return os.Rename(from, to)
	})
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	if err == nil {
		t.Fatal("expected error")
	}
	var rb *RollbackError
	if errors.As(err, &rb) {
		t.Fatalf("rollback succeeded, so the error must not be a RollbackError: %v", err)
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("original not restored: %q", got)
	}
	assertNoLeftovers(t, target)
}

func TestReplaceFile_RollbackFailureIsRollbackError(t *testing.T) {
	target := writeTarget(t, contentOld)
	swapRename(t, func(from, to string) error {
		if to == target {
			return errors.New("simulated: nothing may land on target")
		}
		return os.Rename(from, to)
	})
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	var rb *RollbackError
	if !errors.As(err, &rb) {
		t.Fatalf("want *RollbackError, got %v", err)
	}
	if rb.Update == nil || rb.Rollback == nil {
		t.Fatalf("both errors must be set: %+v", rb)
	}
	if !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("message must say rollback failed: %v", err)
	}
}

func TestPreflightWritable_LeavesNoProbe(t *testing.T) {
	dir := t.TempDir()
	if err := preflightWritable(dir); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("probe file left behind: %v", entries)
	}
}

func TestPreflightWritable_PermissionDenied(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	ro := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(ro, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(ro, 0o700) })
	err := preflightWritable(ro)
	if !errors.Is(err, ErrTargetNotWritable) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), ro) {
		t.Fatalf("message must name the directory: %v", err)
	}
}

func TestPreflightWritable_MissingDir(t *testing.T) {
	err := preflightWritable(filepath.Join(t.TempDir(), "missing"))
	if err == nil || errors.Is(err, ErrTargetNotWritable) {
		t.Fatalf("a missing directory is not a permission problem: %v", err)
	}
}

func TestReplaceFile_FirstRenameFailureLeavesOriginal(t *testing.T) {
	target := writeTarget(t, contentOld)
	swapRename(t, func(from, to string) error {
		if from == target {
			return errors.New("simulated: cannot move target aside")
		}
		return os.Rename(from, to)
	})
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	if err == nil || !strings.Contains(err.Error(), "move old binary aside") {
		t.Fatalf("got %v", err)
	}
	var rb *RollbackError
	if errors.As(err, &rb) {
		t.Fatalf("nothing to roll back, must not be RollbackError: %v", err)
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("original changed: %q", got)
	}
	assertNoLeftovers(t, target)
}

// failingReader errors after yielding a few bytes.
type failingReader struct{ n int }

func (r *failingReader) Read(p []byte) (int, error) {
	if r.n > 0 {
		r.n--
		p[0] = 'x'
		return 1, nil
	}
	return 0, errors.New("simulated read failure")
}

func TestReplaceFile_SourceReadFailureLeavesOriginal(t *testing.T) {
	target := writeTarget(t, contentOld)
	err := replaceFile(target, &failingReader{n: 3}, 0o755)
	if err == nil || !strings.Contains(err.Error(), "write") {
		t.Fatalf("got %v", err)
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("original changed: %q", got)
	}
	assertNoLeftovers(t, target)
}

func TestReplaceFile_UnwritableDirWrapsSentinel(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	target := writeTarget(t, contentOld)
	dir := filepath.Dir(target)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	if !errors.Is(err, ErrTargetNotWritable) {
		t.Fatalf("got %v", err)
	}
	if !strings.Contains(err.Error(), dir) {
		t.Fatalf("message must name the directory: %v", err)
	}
}

func TestReplaceFile_CreateFailureIsNotSentinel(t *testing.T) {
	target := writeTarget(t, contentOld)
	if err := os.Mkdir(target+".new", 0o755); err != nil {
		t.Fatal(err)
	}
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	if err == nil || !strings.Contains(err.Error(), "create ") {
		t.Fatalf("got %v", err)
	}
	if errors.Is(err, ErrTargetNotWritable) {
		t.Fatalf("a directory in the way is not a permission problem: %v", err)
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("original changed: %q", got)
	}
}

func TestReplaceFile_LeftoverRemovalFailureIsReported(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	target := writeTarget(t, contentOld)
	dir := filepath.Dir(target)
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	swapRename(t, func(from, to string) error {
		if err := os.Rename(from, to); err != nil {
			return err
		}
		if to == target {
			// The new binary landed. Freeze the directory so the .old cleanup fails.
			_ = os.Chmod(dir, 0o500)
		}
		return nil
	})
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	if err == nil || !strings.Contains(err.Error(), "could not be removed") {
		t.Fatalf("got %v", err)
	}
	if got := readTarget(t, target); got != contentNew {
		t.Fatalf("update did not land: %q", got)
	}
}
