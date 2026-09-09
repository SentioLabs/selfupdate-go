//nolint:testpackage // exercises unexported helpers
package selfupdate

import (
	"errors"
	"regexp"
	"strings"
	"testing"
)

// managerHomebrew and managerSystem name the literals repeated across the
// fixtures below so their reuse doesn't trip goconst.
const (
	managerHomebrew = "Homebrew"
	managerSystem   = "dpkg/rpm"
)

func TestDetectManaged_Defaults(t *testing.T) {
	cases := []struct {
		path    string
		manager string // "" means not managed
	}{
		{"/opt/homebrew/Cellar/arc/0.15.0/bin/arc", managerHomebrew},
		{"/usr/local/Cellar/arc/0.15.0/bin/arc", managerHomebrew},
		{"/home/linuxbrew/.linuxbrew/Cellar/arc/0.15.0/bin/arc", managerHomebrew},
		{"/opt/homebrew/Cellar/arc/0.15.0/sbin/arc", managerHomebrew},
		{"/opt/homebrew/Cellar/arc/0.15.0/libexec/bin/arc", managerHomebrew},
		{"/usr/bin/arc", managerSystem},
		{"/usr/sbin/arc", managerSystem},
		{"/usr/lib/arc/arc", managerSystem},
		{"/usr/lib64/arc/arc", managerSystem},
		{"/usr/libexec/arc/arc", managerSystem},
		{"/nix/store/abc123-arc-0.15.0/bin/arc", "Nix"},
		{"/usr/local/bin/arc", ""},
		{"/usr/local/lib/arc/arc", ""},
		{"/usr/libx/arc", ""},
		{"/home/u/.local/bin/arc", ""},
		{"/home/u/go/bin/arc", ""},
		{"/home/u/projects/Cellar/bin/arc", ""},
		{"/opt/arc/bin/arc", ""},
	}
	for _, tc := range cases {
		m, ok := detectManaged(tc.path, nil)
		if ok != (tc.manager != "") || m.Manager != tc.manager {
			t.Errorf("%s: got (%q, %v), want manager %q", tc.path, m.Manager, ok, tc.manager)
		}
	}
}

func TestDetectManaged_EmptyListDisables(t *testing.T) {
	if _, ok := detectManaged("/usr/bin/arc", []ManagedInstall{}); ok {
		t.Fatal("an empty non-nil list must disable detection")
	}
}

func TestDetectManaged_CustomList(t *testing.T) {
	list := []ManagedInstall{{
		Pattern: regexp.MustCompile(`^/opt/corp/`),
		Manager: "corp-pkg",
		Hint:    "corp-pkg upgrade {name}",
	}}
	m, ok := detectManaged("/opt/corp/bin/arc", list)
	if !ok || m.Manager != "corp-pkg" {
		t.Fatalf("got (%+v, %v)", m, ok)
	}
	if _, ok := detectManaged("/usr/bin/arc", list); ok {
		t.Fatal("a custom list replaces the defaults, it does not extend them")
	}
}

func TestManagedError(t *testing.T) {
	m, ok := detectManaged("/opt/homebrew/Cellar/arc/0.15.0/bin/arc", nil)
	if !ok {
		t.Fatal("expected Homebrew match")
	}
	err := managedError(m, "arc", "/opt/homebrew/Cellar/arc/0.15.0/bin/arc")
	if !errors.Is(err, ErrManagedInstall) {
		t.Fatalf("must wrap ErrManagedInstall: %v", err)
	}
	for _, want := range []string{managerHomebrew, "brew upgrade arc", "/opt/homebrew/Cellar/arc/0.15.0/bin/arc"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q must contain %q", err.Error(), want)
		}
	}
	if strings.Contains(err.Error(), "{name}") {
		t.Error("placeholder was not substituted")
	}
}
