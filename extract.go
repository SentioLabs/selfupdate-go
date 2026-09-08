package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
)

// ErrEntryNotFound is wrapped when the archive has no entry with the
// requested name.
var ErrEntryNotFound = errors.New("selfupdate: entry not found in archive")

// extractFile copies the tar.gz entry whose cleaned path equals name to
// dst. Both "tool" and "./tool" match "tool"; "bin/tool" does not. Only
// regular files match, so a directory or link with the same name is an
// error. Nothing but dst is ever written, so archive paths never touch
// the filesystem.
func extractFile(archive io.Reader, name string, dst io.Writer) error {
	gz, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("selfupdate: open gzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("%w: %s", ErrEntryNotFound, name)
		}
		if err != nil {
			return fmt.Errorf("selfupdate: read tar: %w", err)
		}
		if path.Clean(hdr.Name) != name {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return fmt.Errorf("selfupdate: archive entry %s is not a regular file", name)
		}
		// The asset was checksum-verified before extraction and a release
		// binary is bounded by its asset size, so no decompression cap.
		if _, err := io.Copy(dst, tr); err != nil { //nolint:gosec // G110: see comment above
			return fmt.Errorf("selfupdate: extract %s: %w", name, err)
		}
		return nil
	}
}
