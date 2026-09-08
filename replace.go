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

// RollbackError reports that the update failed and the original binary
// could not be restored. The install directory needs manual repair.
type RollbackError struct {
	Update   error
	Rollback error
}

func (e *RollbackError) Error() string {
	return fmt.Sprintf("selfupdate: update failed (%v) and rollback failed (%v)", e.Update, e.Rollback)
}

// Unwrap exposes the update error to errors.Is and errors.As.
func (e *RollbackError) Unwrap() error { return e.Update }

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

// replaceFile installs src at target. It writes <target>.new, renames
// target to <target>.old, renames .new over target and removes .old. When
// the final rename fails it moves .old back and returns the rename error.
// If that restore fails too it returns a *RollbackError.
func replaceFile(target string, src io.Reader, mode os.FileMode) error {
	newPath, oldPath := target+".new", target+".old"
	if err := writeFile(newPath, src, mode); err != nil {
		return err
	}
	if err := renameFile(target, oldPath); err != nil {
		_ = os.Remove(newPath)
		return fmt.Errorf("selfupdate: move old binary aside: %w", err)
	}
	if err := renameFile(newPath, target); err != nil {
		_ = os.Remove(newPath)
		if rbErr := renameFile(oldPath, target); rbErr != nil {
			return &RollbackError{Update: err, Rollback: rbErr}
		}
		return fmt.Errorf("selfupdate: install new binary: %w", err)
	}
	if err := os.Remove(oldPath); err != nil {
		return fmt.Errorf("selfupdate: binary was updated but %s could not be removed: %w", oldPath, err)
	}
	return nil
}

// writeFile streams src into path with mode, removing the file on failure.
func writeFile(path string, src io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		if errors.Is(err, os.ErrPermission) {
			return fmt.Errorf("%w: %s", ErrTargetNotWritable, filepath.Dir(path))
		}
		return fmt.Errorf("selfupdate: create %s: %w", path, err)
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
