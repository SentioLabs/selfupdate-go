//go:build darwin

package selfupdate

import (
	"fmt"
	"os/exec"
)

// signBinary applies an ad-hoc code signature so macOS runs a binary that
// arrived by download rather than through a signed installer. The Go
// linker already ad-hoc signs darwin/arm64 output, so this keeps parity
// with the install script rather than adding a new requirement.
func signBinary(path string) error {
	out, err := exec.Command("codesign", "--force", "--sign", "-", path).CombinedOutput() //nolint:gosec // fixed program; path is the resolved target
	if err != nil {
		return fmt.Errorf("codesign: %w: %s", err, out)
	}
	return nil
}
