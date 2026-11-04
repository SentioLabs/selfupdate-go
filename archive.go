package selfupdate

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// DefaultAssetTemplate matches goreleaser's default archive name.
const DefaultAssetTemplate = "{name}_{version}_{os}_{arch}.tar.gz"

// DefaultChecksumAsset is goreleaser's default checksum file name.
const DefaultChecksumAsset = "checksums.txt"

// maxChecksumBytes caps a checksum file the release lists no size for,
// on the VerifyChecksums path.
const maxChecksumBytes = 1 << 20

// verifyDirMode is the permission set on the subdirectory of Prepare's
// temp dir that holds the checksum file passed to VerifyChecksums.
const verifyDirMode os.FileMode = 0o700

// responseHeaderTimeout bounds how long the default client waits for
// response headers. Bodies stream until ctx is cancelled.
const responseHeaderTimeout = 30 * time.Second

// binaryMode is the permission set on the installed binary.
const binaryMode os.FileMode = 0o755

// ArchiveInstaller downloads the release asset for the running platform,
// verifies it against the release's checksum file, extracts the binary and
// replaces the running executable in place. It needs no shell, curl or
// install script.
type ArchiveInstaller struct {
	Name          string // binary name inside the archive; default: base name of the resolved target
	AssetTemplate string // default DefaultAssetTemplate; {version} has no leading v
	ChecksumAsset string // default DefaultChecksumAsset
	SkipChecksum  bool   // disable verification for repos that publish no checksum file
	// VerifyChecksums, when set, runs on the downloaded checksum file
	// before it is trusted, for example to check a signature or an
	// attestation. path is the file inside Prepare's temp dir. An error
	// stops the update before the archive is downloaded. A checksum file
	// the release lists no size for is capped at 1 MiB.
	VerifyChecksums func(ctx context.Context, rel Release, path string) error
	TargetPath      string           // default: os.Executable(); symlinks are always resolved
	Managed         []ManagedInstall // nil means DefaultManagedInstalls; empty slice disables the check
	Client          *http.Client     // default: a shared DefaultTransport clone with a 30s ResponseHeaderTimeout
	Out             io.Writer        // progress lines; default os.Stdout
}

// defaultClient is built on first use and shared by every ArchiveInstaller
// without an explicit Client. Prepare never writes to the installer, so one
// value may serve concurrent goroutines.
var defaultClient = sync.OnceValue(newDefaultClient)

// archiveStaged holds a verified, extracted binary waiting to be renamed
// over the target.
type archiveStaged struct {
	installer *ArchiveInstaller
	tag       string
	target    string
	dir       string // temp dir holding the download and the extracted binary
	binary    string // extracted binary inside dir
}

// Prepare resolves the target, refuses managed or unwritable locations,
// then downloads, verifies and extracts the platform asset into a temp
// directory. The running binary is untouched. VerifyChecksums together
// with SkipChecksum is refused before anything else.
func (a *ArchiveInstaller) Prepare(ctx context.Context, rel Release) (Staged, error) {
	if a.VerifyChecksums != nil && a.SkipChecksum {
		return nil, errors.New("selfupdate: VerifyChecksums needs the checksum file, but SkipChecksum is set")
	}
	target, err := a.target()
	if err != nil {
		return nil, err
	}
	name := a.name(target)
	if m, ok := detectManaged(target, a.Managed); ok {
		return nil, managedError(m, name, target)
	}
	if err := preflightWritable(filepath.Dir(target)); err != nil {
		return nil, err
	}
	asset, err := selectAsset(rel, a.assetName(name, rel.Tag))
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "selfupdate-*")
	if err != nil {
		return nil, fmt.Errorf("selfupdate: create temp dir: %w", err)
	}
	staged := &archiveStaged{installer: a, tag: rel.Tag, target: target, dir: dir}
	if err := a.stage(ctx, rel, asset, name, staged); err != nil {
		_ = staged.Close()
		return nil, err
	}
	return staged, nil
}

// Sweep is retained for compatibility and does nothing. Replacement files
// have unique names and are removed on ordinary failures. Files left by a
// crash cannot safely be distinguished from another update's active files.
//
// Deprecated: ArchiveInstaller no longer requires sweeping.
func (a *ArchiveInstaller) Sweep() error {
	return nil
}

// stage downloads asset into staged.dir, verifies it unless SkipChecksum,
// and extracts name beside it. With VerifyChecksums set, the checksum file
// is downloaded and passed to the hook before the archive is requested.
func (a *ArchiveInstaller) stage(
	ctx context.Context, rel Release, asset Asset, name string, staged *archiveStaged,
) error {
	archivePath := filepath.Join(staged.dir, asset.Name)
	var err error
	if a.VerifyChecksums != nil {
		err = a.downloadTrusted(ctx, rel, asset, archivePath, staged.dir)
	} else {
		err = a.downloadChecked(ctx, rel, asset, archivePath)
	}
	if err != nil {
		return err
	}
	staged.binary = filepath.Join(staged.dir, name)
	return extractTo(archivePath, name, staged.binary)
}

// downloadChecked downloads asset to path, then streams the checksum file
// to verify it unless SkipChecksum. It is the path without VerifyChecksums.
func (a *ArchiveInstaller) downloadChecked(ctx context.Context, rel Release, asset Asset, path string) error {
	sum, err := a.download(ctx, asset, path)
	if err != nil {
		return err
	}
	if !a.SkipChecksum {
		if err := a.verify(ctx, rel, asset.Name, sum); err != nil {
			return err
		}
		a.printf("Verified %s\n", asset.Name)
	}
	return nil
}

// downloadTrusted saves the checksum file into a verify subdirectory of
// dir and passes it to VerifyChecksums. Only after the hook accepts it is
// asset downloaded to path and checked against the saved file, so the
// bytes the hook checked are the bytes the archive is verified against.
// The saved file keeps only the base of the asset name, so it can neither
// escape dir nor collide with the archive.
func (a *ArchiveInstaller) downloadTrusted(
	ctx context.Context, rel Release, asset Asset, path, dir string,
) error {
	checksums, err := selectAsset(rel, a.checksumAsset())
	if err != nil {
		return err
	}
	verifyDir := filepath.Join(dir, "verify")
	if err := os.Mkdir(verifyDir, verifyDirMode); err != nil {
		return fmt.Errorf("selfupdate: create verify dir: %w", err)
	}
	checksumsPath := filepath.Join(verifyDir, filepath.Base(checksums.Name))
	if _, err := a.downloadTo(ctx, checksums, checksumsPath, io.Discard, maxChecksumBytes); err != nil {
		return err
	}
	if err := a.VerifyChecksums(ctx, rel, checksumsPath); err != nil {
		return fmt.Errorf("selfupdate: verify %s: %w", checksums.Name, err)
	}
	sum, err := a.download(ctx, asset, path)
	if err != nil {
		return err
	}
	if err := verifySaved(checksumsPath, asset.Name, sum); err != nil {
		return err
	}
	a.printf("Verified %s\n", asset.Name)
	return nil
}

// download prints the download line and streams asset to path with a
// progress bar on a terminal. It returns the SHA-256 of the bytes written.
func (a *ArchiveInstaller) download(ctx context.Context, asset Asset, path string) ([]byte, error) {
	a.printf("Downloading %s (%s)...\n", asset.Name, formatMB(asset.Size))
	return a.downloadTo(ctx, asset, path, a.out(), 0)
}

// downloadTo streams asset.URL to path, drawing progress to progressOut
// when it is a terminal, and returns the SHA-256 of the bytes written.
// When the release lists a size the body must match it exactly. When it
// lists none, a positive maxUnlisted caps the body and zero leaves it
// unbounded.
func (a *ArchiveInstaller) downloadTo(
	ctx context.Context, asset Asset, path string, progressOut io.Writer, maxUnlisted int64,
) ([]byte, error) {
	hash := sha256.New()
	err := a.fetch(ctx, asset.URL, func(resp *http.Response) error {
		f, err := os.Create(path) // path is inside the temp dir created by Prepare
		if err != nil {
			return fmt.Errorf("selfupdate: create download: %w", err)
		}
		progress := newProgressWriter(progressOut, resp.ContentLength)
		copyErr := copyBody(io.MultiWriter(f, hash, progress), resp.Body, asset.Size, maxUnlisted)
		progress.Finish()
		if closeErr := f.Close(); copyErr == nil {
			copyErr = closeErr
		}
		if copyErr != nil {
			return fmt.Errorf("selfupdate: download %s: %w", asset.Name, copyErr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return hash.Sum(nil), nil
}

// copyBody streams body into dst. It applies copyBounded when size is
// listed, and copyCapped when size is not and maxUnlisted is positive.
func copyBody(dst io.Writer, body io.Reader, size, maxUnlisted int64) error {
	if size <= 0 && maxUnlisted > 0 {
		return copyCapped(dst, body, maxUnlisted)
	}
	return copyBounded(dst, body, size)
}

// copyCapped streams body into dst and reads at most limit+1 bytes. A
// body longer than limit is an error.
func copyCapped(dst io.Writer, body io.Reader, limit int64) error {
	n, err := io.Copy(dst, io.LimitReader(body, limit+1))
	if err != nil {
		return err
	}
	if n > limit {
		return fmt.Errorf("body exceeds %d bytes", limit)
	}
	return nil
}

// copyBounded streams body into dst. A positive size caps the read one
// byte past it, so a hostile payload cannot fill the temp dir before the
// checksum rejects it, and a body of any other length is an error. A zero
// or negative size means the release listed none and the body is unbounded.
func copyBounded(dst io.Writer, body io.Reader, size int64) error {
	if size <= 0 {
		_, err := io.Copy(dst, body)
		return err
	}
	n, err := io.Copy(dst, io.LimitReader(body, size+1))
	if err != nil {
		return err
	}
	if n != size {
		return fmt.Errorf("body is %d bytes, release lists %d", n, size)
	}
	return nil
}

// verify downloads the checksum asset and checks sum against assetName's entry.
func (a *ArchiveInstaller) verify(ctx context.Context, rel Release, assetName string, sum []byte) error {
	checksums, err := selectAsset(rel, a.checksumAsset())
	if err != nil {
		return err
	}
	return a.fetch(ctx, checksums.URL, func(resp *http.Response) error {
		sums, err := parseChecksums(resp.Body)
		if err != nil {
			return err
		}
		return verifyChecksum(sums, assetName, sum)
	})
}

// verifySaved checks sum against assetName's entry in the checksum file
// saved at path.
func verifySaved(path, assetName string, sum []byte) error {
	f, err := os.Open(path) // path is inside the temp dir created by Prepare
	if err != nil {
		return fmt.Errorf("selfupdate: open checksums: %w", err)
	}
	defer func() { _ = f.Close() }()
	sums, err := parseChecksums(f)
	if err != nil {
		return err
	}
	return verifyChecksum(sums, assetName, sum)
}

// fetch GETs url and hands the 200 response to fn. The body is closed
// when fn returns. Any other status is an error.
func (a *ArchiveInstaller) fetch(ctx context.Context, url string, fn func(*http.Response) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("selfupdate: build request: %w", err)
	}
	resp, err := a.client().Do(req)
	if err != nil {
		return fmt.Errorf("selfupdate: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("selfupdate: GET %s returned status %d", url, resp.StatusCode)
	}
	return fn(resp)
}

// extractTo pulls entry name out of the tar.gz at archivePath into dst.
func extractTo(archivePath, name, dst string) error {
	in, err := os.Open(archivePath) // path is inside the temp dir created by Prepare
	if err != nil {
		return fmt.Errorf("selfupdate: open archive: %w", err)
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, binaryMode) // same temp dir
	if err != nil {
		return fmt.Errorf("selfupdate: create extracted binary: %w", err)
	}
	if err := extractFile(in, name, out); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// selectAsset returns the asset named exactly name. Substring matches are
// rejected on purpose: tool_1.2.3_linux_amd64.tar.gz.sig must not shadow
// the tarball. The error lists the available names.
func selectAsset(rel Release, name string) (Asset, error) {
	names := make([]string, 0, len(rel.Assets))
	for _, a := range rel.Assets {
		if a.Name == name {
			return a, nil
		}
		names = append(names, a.Name)
	}
	return Asset{}, fmt.Errorf("selfupdate: release %s has no asset %s (available: %s)",
		rel.Tag, name, strings.Join(names, ", "))
}

// assetName renders AssetTemplate for name, tag and the running platform.
func (a *ArchiveInstaller) assetName(name, tag string) string {
	tmpl := a.AssetTemplate
	if tmpl == "" {
		tmpl = DefaultAssetTemplate
	}
	return strings.NewReplacer(
		"{name}", name,
		"{version}", strings.TrimPrefix(tag, "v"),
		"{os}", runtime.GOOS,
		"{arch}", runtime.GOARCH,
	).Replace(tmpl)
}

// target returns TargetPath, or the running executable, with symlinks
// resolved so a Homebrew or ~/.local/bin link is followed to the real file.
func (a *ArchiveInstaller) target() (string, error) {
	p := a.TargetPath
	if p == "" {
		exe, err := os.Executable()
		if err != nil {
			return "", fmt.Errorf("selfupdate: locate running binary: %w", err)
		}
		p = exe
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("selfupdate: resolve %s: %w", p, err)
	}
	return resolved, nil
}

// name returns Name, or the base name of the resolved target.
func (a *ArchiveInstaller) name(target string) string {
	if a.Name != "" {
		return a.Name
	}
	return filepath.Base(target)
}

// checksumAsset returns ChecksumAsset, or goreleaser's default name.
func (a *ArchiveInstaller) checksumAsset() string {
	if a.ChecksumAsset != "" {
		return a.ChecksumAsset
	}
	return DefaultChecksumAsset
}

// client returns Client, or the shared default. It never writes to a.
func (a *ArchiveInstaller) client() *http.Client {
	if a.Client != nil {
		return a.Client
	}
	return defaultClient()
}

// newDefaultClient clones the default transport and bounds the wait for
// response headers so a dead connection fails instead of hanging forever.
func newDefaultClient() *http.Client {
	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		return &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: responseHeaderTimeout}}
	}
	tr = tr.Clone()
	tr.ResponseHeaderTimeout = responseHeaderTimeout
	return &http.Client{Transport: tr}
}

// out returns Out, or os.Stdout.
func (a *ArchiveInstaller) out() io.Writer {
	if a.Out != nil {
		return a.Out
	}
	return os.Stdout
}

// printf writes one progress line to out.
func (a *ArchiveInstaller) printf(format string, args ...any) {
	_, _ = fmt.Fprintf(a.out(), format, args...)
}

// Commit renames the extracted binary over the target and re-signs it on
// macOS. A signing failure is a warning, not an error.
func (s *archiveStaged) Commit(_ context.Context) error {
	f, err := os.Open(s.binary) // path is inside our own temp dir
	if err != nil {
		return fmt.Errorf("selfupdate: open staged binary: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := replaceFile(s.target, f, binaryMode); err != nil {
		return err
	}
	if err := signBinary(s.target); err != nil {
		s.installer.printf("Warning: could not re-sign %s: %v\n", s.target, err)
	}
	s.installer.printf("Installed %s %s to %s\n", filepath.Base(s.target), s.tag, s.target)
	return nil
}

// Close removes the temp directory. It is safe to call more than once.
func (s *archiveStaged) Close() error {
	if s.dir == "" {
		return nil
	}
	err := os.RemoveAll(s.dir)
	s.dir = ""
	return err
}
