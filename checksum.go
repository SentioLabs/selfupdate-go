package selfupdate

import (
	"bufio"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrChecksumMismatch is wrapped when a downloaded asset's digest differs
// from the one listed in the checksum file.
var ErrChecksumMismatch = errors.New("selfupdate: checksum mismatch")

// checksumFields is the field count of a well-formed line: digest and name.
const checksumFields = 2

// parseChecksums reads "<hex>  <filename>" lines, as written by goreleaser
// and sha256sum, into a filename -> lowercase hex map. A leading "*" on
// the filename (GNU binary mode) is dropped. Blank lines are skipped. Any
// other shape is an error that names the line.
func parseChecksums(r io.Reader) (map[string]string, error) {
	sums := make(map[string]string)
	sc := bufio.NewScanner(r)
	for line := 1; sc.Scan(); line++ {
		text := strings.TrimSpace(sc.Text())
		if text == "" {
			continue
		}
		fields := strings.Fields(text)
		if len(fields) != checksumFields {
			return nil, fmt.Errorf("selfupdate: checksum line %d: want %d fields, got %d",
				line, checksumFields, len(fields))
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return nil, fmt.Errorf("selfupdate: checksum line %d: %w", line, err)
		}
		sums[strings.TrimPrefix(fields[1], "*")] = strings.ToLower(fields[0])
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("selfupdate: read checksums: %w", err)
	}
	return sums, nil
}

// verifyChecksum compares sum against the entry for name. A missing entry
// is an error distinct from ErrChecksumMismatch.
func verifyChecksum(sums map[string]string, name string, sum []byte) error {
	want, ok := sums[name]
	if !ok {
		return fmt.Errorf("selfupdate: no checksum listed for %s", name)
	}
	if got := hex.EncodeToString(sum); got != want {
		return fmt.Errorf("%w for %s: want %s, got %s", ErrChecksumMismatch, name, want, got)
	}
	return nil
}
