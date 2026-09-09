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

// replaceFile installs src at target. It writes <target>.new beside the
// target and renames it over target. rename(2) replaces the file in one
// step on Linux and macOS, so a failed rename leaves the original in
// place and a crash at any point leaves the target present. The running
// process keeps its unlinked inode and is unaffected.
func replaceFile(target string, src io.Reader, mode os.FileMode) error {
	newPath := target + ".new"
	if err := removeStaleNew(target); err != nil {
		return err
	}
	if err := writeFile(newPath, src, mode); err != nil {
		return err
	}
	if err := renameFile(newPath, target); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("selfupdate: install new binary: %w", err)
	}
	return nil
}

// removeStaleNew deletes <target>.new when it is a regular file. An
// interrupted install can leave one behind. The .new suffix is ours only
// by convention, so a directory or symlink by that name is left alone and
// writeFile reports it.
func removeStaleNew(target string) error {
	newPath := target + ".new"
	info, err := os.Lstat(newPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("selfupdate: inspect %s: %w", newPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	if err := os.Remove(newPath); err != nil {
		return fmt.Errorf("selfupdate: remove stale %s: %w", newPath, err)
	}
	return nil
}

// writeFile streams src into path with mode, removing the file on failure.
// The mode is applied with Chmod after creation so the process umask cannot
// narrow it.
func writeFile(path string, src io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%w: %s", ErrTargetNotWritable, filepath.Dir(path))
		}
		return fmt.Errorf("selfupdate: create %s: %w", path, err)
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("selfupdate: set mode on %s: %w", path, err)
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return fmt.Errorf("selfupdate: write %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return fmt.Errorf("selfupdate: close %s: %w", path, err)
	}
	return nil
}
