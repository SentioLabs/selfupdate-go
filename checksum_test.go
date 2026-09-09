//nolint:testpackage // exercises unexported helpers
package selfupdate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

const (
	linuxTarball  = "tool_1.2.3_linux_amd64.tar.gz"
	darwinTarball = "tool_1.2.3_darwin_arm64.tar.gz"
)

func TestParseChecksums_GoreleaserAndBinaryMode(t *testing.T) {
	in := "0123abcd  " + linuxTarball + "\n" +
		"\n" +
		"ABCD0123 *" + darwinTarball + "\r\n"
	sums, err := parseChecksums(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 {
		t.Fatalf("got %d entries: %v", len(sums), sums)
	}
	if sums[linuxTarball] != "0123abcd" {
		t.Errorf("two-space separator: %q", sums[linuxTarball])
	}
	if sums[darwinTarball] != "abcd0123" {
		t.Errorf("binary-mode marker or case folding: %v", sums)
	}
}

func TestParseChecksums_RejectsMalformedLines(t *testing.T) {
	for _, in := range []string{"onlyonefield\n", "zz  name\n", "0123 two words\n"} {
		if _, err := parseChecksums(strings.NewReader(in)); err == nil {
			t.Errorf("input %q must be rejected", in)
		}
	}
}

func TestParseChecksums_Empty(t *testing.T) {
	sums, err := parseChecksums(strings.NewReader(""))
	if err != nil || len(sums) != 0 {
		t.Fatalf("got %v, %v", sums, err)
	}
}

func TestVerifyChecksum(t *testing.T) {
	sum := sha256.Sum256([]byte("hello"))
	sums := map[string]string{linuxTarball: hex.EncodeToString(sum[:])}

	if err := verifyChecksum(sums, linuxTarball, sum[:]); err != nil {
		t.Fatalf("match: %v", err)
	}

	other := sha256.Sum256([]byte("tampered"))
	err := verifyChecksum(sums, linuxTarball, other[:])
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("mismatch: got %v", err)
	}
	if !strings.Contains(err.Error(), linuxTarball) {
		t.Fatalf("mismatch error must name the asset: %v", err)
	}

	err = verifyChecksum(sums, darwinTarball, sum[:])
	if err == nil || errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("missing entry must be its own error, got %v", err)
	}
}
