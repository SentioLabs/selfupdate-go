//nolint:testpackage // exercises unexported helpers and swaps renameFile
package selfupdate

import (
	"errors"
	"fmt"
	"io"
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
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "."+filepath.Base(target)+".new-") {
			t.Errorf("temporary replacement left behind: %s", entry.Name())
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

func TestReplaceFile_RenameFailureLeavesOriginal(t *testing.T) {
	target := writeTarget(t, contentOld)
	swapRename(t, func(_, _ string) error {
		return errors.New("simulated rename failure")
	})
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	if err == nil || !strings.Contains(err.Error(), "install new binary") {
		t.Fatalf("got %v", err)
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("original changed: %q", got)
	}
	assertNoLeftovers(t, target)
}

// TestReplaceFile_NeverMovesTargetAside pins the single-rename design: the
// only rename is the temporary file over target, so target is never moved away.
func TestReplaceFile_NeverMovesTargetAside(t *testing.T) {
	target := writeTarget(t, contentOld)
	var renames [][2]string
	swapRename(t, func(from, to string) error {
		renames = append(renames, [2]string{from, to})
		return os.Rename(from, to)
	})
	if err := replaceFile(target, strings.NewReader(contentNew), 0o755); err != nil {
		t.Fatal(err)
	}
	if len(renames) != 1 || renames[0][1] != target {
		t.Fatalf("renames %v, want exactly one rename over target", renames)
	}
	from := renames[0][0]
	if filepath.Dir(from) != filepath.Dir(target) || !strings.HasPrefix(filepath.Base(from), ".tool.new-") {
		t.Fatalf("replacement must be a unique sibling: %s", from)
	}
}

// writeStaleNew leaves a partial <target>.new beside target, as an install
// interrupted before its rename would.
func writeStaleNew(t *testing.T, target string) {
	t.Helper()
	if err := os.WriteFile(target+".new", []byte("partial"), 0o755); err != nil { //nolint:gosec // fixture
		t.Fatal(err)
	}
}

func TestReplaceFile_LeavesLegacyNewAlone(t *testing.T) {
	for _, directory := range []bool{false, true} {
		t.Run(fmt.Sprintf("directory=%t", directory), func(t *testing.T) {
			checkLegacyNew(t, directory)
		})
	}
}

func checkLegacyNew(t *testing.T, directory bool) {
	t.Helper()
	target := writeTarget(t, contentOld)
	if directory {
		if err := os.Mkdir(target+".new", 0o755); err != nil {
			t.Fatal(err)
		}
	} else {
		writeStaleNew(t, target)
	}
	if err := replaceFile(target, strings.NewReader(contentNew), 0o755); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(target + ".new")
	if err != nil || info.IsDir() != directory {
		t.Fatalf("legacy entry changed: %v, %v", info, err)
	}
	if !directory && readTarget(t, target+".new") != "partial" {
		t.Fatal("legacy file changed")
	}
	if readTarget(t, target) != contentNew {
		t.Fatal("replacement not installed")
	}
	assertNoLeftovers(t, target)
}

// A planted legacy symlink must never redirect writes or chmod, including
// when writing the replacement fails partway through.
func TestReplaceFile_LeavesSymlinkVictimUntouched(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprintf("write_failure=%t", fail), func(t *testing.T) {
			checkSymlinkVictim(t, fail)
		})
	}
}

func checkSymlinkVictim(t *testing.T, fail bool) {
	t.Helper()
	target := writeTarget(t, contentOld)
	victim := filepath.Join(t.TempDir(), "unrelated")
	if err := os.WriteFile(victim, []byte("unrelated contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, target+".new"); err != nil {
		t.Fatal(err)
	}
	var src io.Reader = strings.NewReader(contentNew)
	if fail {
		src = &failingReader{n: 3}
	}
	err := replaceFile(target, src, 0o755)
	if (err != nil) != fail {
		t.Fatalf("replace error: %v", err)
	}
	if readTarget(t, victim) != "unrelated contents" {
		t.Fatal("symlink victim overwritten")
	}
	info, err := os.Stat(victim)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("victim permissions changed: %v", info.Mode())
	}
	link, err := os.Readlink(target + ".new")
	if err != nil || link != victim {
		t.Fatalf("legacy symlink changed: %q, %v", link, err)
	}
	info, err = os.Lstat(target)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("target is not a regular file: %v", info.Mode())
	}
	want := contentNew
	if fail {
		want = contentOld
	}
	if readTarget(t, target) != want {
		t.Fatal("unexpected target contents")
	}
	assertNoLeftovers(t, target)
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
	target := filepath.Join(t.TempDir(), "missing", "tool")
	err := replaceFile(target, strings.NewReader(contentNew), 0o755)
	if err == nil || !strings.Contains(err.Error(), "create ") {
		t.Fatalf("got %v", err)
	}
	if errors.Is(err, ErrTargetNotWritable) {
		t.Fatalf("a missing directory is not a permission problem: %v", err)
	}
}
