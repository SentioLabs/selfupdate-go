package selfupdate

import (
	"strings"

	"golang.org/x/mod/semver"
)

// devVersion is what development builds normalize to. It sorts below every
// real release so a dev binary is always offered an update.
const devVersion = "v0.0.0-dev"

// NormalizeVersion adds a leading v and maps "" and "dev" to v0.0.0-dev.
func NormalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	if v == "" || v == "dev" {
		return devVersion
	}
	if !strings.HasPrefix(v, "v") {
		return "v" + v
	}
	return v
}

// Compare uses semantic version order, treating legacy rcN counters as rc.N.
// The result is 0 when current and latest are equal, positive when latest is
// newer (an update is available) and negative when current is newer than latest.
func Compare(current, latest string) int {
	return semver.Compare(comparisonVersion(latest), comparisonVersion(current))
}

// comparisonVersion canonicalizes RC counters only for comparison. Public
// version strings and release tags keep their original spelling.
func comparisonVersion(v string) string {
	v = NormalizeVersion(v)
	prerelease := semver.Prerelease(v)
	counter, ok := strings.CutPrefix(prerelease, "-rc")
	if !ok || counter == "" {
		return v
	}
	for _, digit := range counter {
		if digit < '0' || digit > '9' {
			return v
		}
	}
	// Numeric semver identifiers cannot have leading zeroes. Keep the counter
	// as a string so even values larger than a machine integer compare exactly.
	counter = strings.TrimLeft(counter, "0")
	if counter == "" {
		counter = "0"
	}
	return strings.Replace(v, prerelease, "-rc."+counter, 1)
}
