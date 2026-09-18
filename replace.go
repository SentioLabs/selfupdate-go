package selfupdate

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// ErrTargetNotWritable is wrapped when the directory holding the running
// binary refuses a new file.
var ErrTargetNotWritable = errors.New("selfupdate: target directory is not writable")

// renameFile is os.Rename. Tests swap it to simulate a failed install.
var renameFile = os.Rename

// preflightWritable creates and removes a temp file in dir. A permission
// error is wrapped with ErrTargetNotWritable so callers can print a
// specific hint. Other errors are returned as they are.
func preflightWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".selfupdate-preflight-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%w: %s", ErrTargetNotWritable, dir)
		}
		return fmt.Errorf("selfupdate: probe %s: %w", dir, err)
	}
	name := f.Name()
	_ = f.Close()
	return os.Remove(name)
}

// replaceFile installs src at target using an exclusively created temporary
// file beside target. Writing through its original handle avoids following
// pre-existing symlinks. A single rename replaces target on Linux and macOS;
// failures before the rename leave the original binary in place.
func replaceFile(target string, src io.Reader, mode os.FileMode) error {
	dir := filepath.Dir(target)
	f, err := os.CreateTemp(dir, "."+filepath.Base(target)+".new-*")
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%w: %s", ErrTargetNotWritable, dir)
		}
		return fmt.Errorf("selfupdate: create replacement in %s: %w", dir, err)
	}
	newPath := f.Name()
	defer func() { _ = os.Remove(newPath) }()
	if err := writeReplacement(f, src, mode); err != nil {
		return err
	}
	if err := renameFile(newPath, target); err != nil {
		return fmt.Errorf("selfupdate: install new binary: %w", err)
	}
	return nil
}

// writeReplacement writes and closes the already-open temporary file. Chmod
// applies the final mode after writing so the process umask cannot narrow it.
func writeReplacement(f *os.File, src io.Reader, mode os.FileMode) error {
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		return fmt.Errorf("selfupdate: write %s: %w", f.Name(), err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return fmt.Errorf("selfupdate: set mode on %s: %w", f.Name(), err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("selfupdate: close %s: %w", f.Name(), err)
	}
	return nil
}
