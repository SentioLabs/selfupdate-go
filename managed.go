package selfupdate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// ManagedInstall describes a location owned by a package manager. Replacing
// a binary there would desynchronize the manager's file list, and its next
// upgrade would silently revert the binary.
type ManagedInstall struct {
	Pattern *regexp.Regexp // matched against the resolved target path
	Manager string         // "Homebrew", "dpkg/rpm", "Nix"
	Hint    string         // "{name}" is replaced with the binary name
}

// DefaultManagedInstalls covers Homebrew and Linuxbrew (a Cellar formula
// directory), system package directories (/usr/bin, /usr/sbin, /usr/lib,
// /usr/lib64, /usr/libexec) and the Nix store. /usr/local/bin and
// ~/.local/bin are not managed. Patterns are case-sensitive; the target
// comes from EvalSymlinks and so carries on-disk casing.
var DefaultManagedInstalls = []ManagedInstall{
	{
		// <prefix>/Cellar/<formula>/<version>/... Anything below the
		// version directory is Homebrew's, whether bin, sbin or libexec.
		// Requiring the two segments keeps a project directory that
		// happens to be named Cellar from being refused.
		Pattern: regexp.MustCompile(`/Cellar/[^/]+/[^/]+/`),
		Manager: "Homebrew",
		Hint:    "brew upgrade {name}",
	},
	{
		Pattern: regexp.MustCompile(`^/usr/(s?bin|lib(64|exec)?)/`),
		Manager: "dpkg/rpm",
		Hint:    "upgrade {name} with your system package manager (apt, dnf, pacman)",
	},
	{
		Pattern: regexp.MustCompile(`^/nix/store/`),
		Manager: "Nix",
		Hint:    "nix profile upgrade {name}",
	},
}

// ErrManagedInstall is the sentinel wrapped when the target is managed.
var ErrManagedInstall = errors.New("selfupdate: binary is managed by a package manager")

// detectManaged returns the first entry in list whose Pattern matches
// target, or false. A nil list means DefaultManagedInstalls; an empty
// non-nil list disables detection.
func detectManaged(target string, list []ManagedInstall) (ManagedInstall, bool) {
	if list == nil {
		list = DefaultManagedInstalls
	}
	for _, m := range list {
		if m.Pattern != nil && m.Pattern.MatchString(target) {
			return m, true
		}
	}
	return ManagedInstall{}, false
}

// managedError builds the error returned when detectManaged matches. target
// must be the same resolved path that was passed to detectManaged, so the
// message names the file the check examined.
func managedError(m ManagedInstall, name, target string) error {
	hint := strings.ReplaceAll(m.Hint, "{name}", name)
	return fmt.Errorf("%w: %s is installed by %s at %s; run '%s' instead",
		ErrManagedInstall, name, m.Manager, target, hint)
}
