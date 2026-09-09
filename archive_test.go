//nolint:testpackage // exercises the unexported staged type and helpers
package selfupdate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// --- Contract assertions ---
var (
	_ Installer = (*ArchiveInstaller)(nil)
	_ Staged    = (*archiveStaged)(nil)
)

const (
	newBinary     = "#!/bin/sh\necho new\n"
	checksumsPath = "/checksums.txt"
)

// archiveFixture serves one release: a tarball for the running platform,
// a checksums.txt, and decoys that must never be selected.
type archiveFixture struct {
	srv       *httptest.Server
	rel       Release
	assetName string
	mu        sync.Mutex
	requests  []string
}

func (fx *archiveFixture) record(path string) {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	fx.requests = append(fx.requests, path)
}

func (fx *archiveFixture) requested() []string {
	fx.mu.Lock()
	defer fx.mu.Unlock()
	return append([]string(nil), fx.requests...)
}

func newArchiveFixture(t *testing.T, body string, tamper bool) *archiveFixture {
	t.Helper()
	fx := &archiveFixture{assetName: fmt.Sprintf("tool_1.2.3_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)}
	archive := tarGz(t, tarEntry{name: "LICENSE.txt", body: "license text"}, tarEntry{name: testRepo, body: body})
	sum := sha256.Sum256(archive)
	if tamper {
		sum[0] ^= 0xff
	}
	checksums := hex.EncodeToString(sum[:]) + "  " + fx.assetName + "\n"

	mux := http.NewServeMux()
	mux.HandleFunc("/"+fx.assetName, func(w http.ResponseWriter, r *http.Request) {
		fx.record(r.URL.Path)
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		_, _ = w.Write(archive)
	})
	mux.HandleFunc(checksumsPath, func(w http.ResponseWriter, r *http.Request) {
		fx.record(r.URL.Path)
		_, _ = io.WriteString(w, checksums)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fx.record(r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	})
	fx.srv = httptest.NewServer(mux)
	t.Cleanup(fx.srv.Close)

	fx.rel = Release{Tag: tagV123, Assets: []Asset{
		{Name: fx.assetName + ".sig", URL: fx.srv.URL + "/never.sig", Size: 1},
		{Name: "tool_1.2.3_linux_amd64.deb", URL: fx.srv.URL + "/never.deb", Size: 1},
		{Name: fx.assetName, URL: fx.srv.URL + "/" + fx.assetName, Size: int64(len(archive))},
		{Name: DefaultChecksumAsset, URL: fx.srv.URL + checksumsPath, Size: int64(len(checksums))},
	}}
	return fx
}

// newTestInstaller targets an explicit file and disables managed-path
// detection so an unusual TMPDIR cannot trip the defaults.
func newTestInstaller(target string, out io.Writer) *ArchiveInstaller {
	return &ArchiveInstaller{Name: testRepo, TargetPath: target, Out: out, Managed: []ManagedInstall{}}
}

func TestArchiveInstaller_PrepareThenCommit(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	target := writeTarget(t, contentOld)
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	inst := newTestInstaller(target, &out)

	staged, err := inst.Prepare(context.Background(), fx.rel)
	if err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("Prepare must not touch the target, got %q", got)
	}
	if got := fx.requested(); len(got) != 2 || got[0] != "/"+fx.assetName || got[1] != checksumsPath {
		t.Fatalf("requests %v", got)
	}

	if err := staged.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != newBinary {
		t.Fatalf("target after Commit: %q", got)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("mode %v", info.Mode().Perm())
	}
	assertNoLeftovers(t, target)

	dir := staged.(*archiveStaged).dir
	if err := staged.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temp dir %s must be removed by Close", dir)
	}

	wants := []string{
		"Downloading " + fx.assetName,
		"Verified " + fx.assetName,
		"Installed tool " + tagV123 + " to " + resolvedTarget,
	}
	for _, want := range wants {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
	if strings.Count(out.String(), "Downloading") != 1 || strings.Contains(out.String(), "\r") {
		t.Fatalf("non-terminal output must be one download line with no redraws:\n%q", out.String())
	}
}

func TestArchiveInstaller_ChecksumMismatchLeavesTarget(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, true)
	target := writeTarget(t, contentOld)
	staged, err := newTestInstaller(target, io.Discard).Prepare(context.Background(), fx.rel)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("got %v", err)
	}
	if staged != nil {
		t.Fatal("no staged update may be returned on failure")
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("target changed: %q", got)
	}
}

func TestArchiveInstaller_MissingChecksumAsset(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	rel := fx.rel
	rel.Assets = rel.Assets[:3] // drop checksums.txt
	target := writeTarget(t, contentOld)
	_, err := newTestInstaller(target, io.Discard).Prepare(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), DefaultChecksumAsset) {
		t.Fatalf("got %v", err)
	}
	if got := readTarget(t, target); got != contentOld {
		t.Fatalf("target changed: %q", got)
	}
}

func TestArchiveInstaller_SkipChecksum(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, true) // tampered checksum file, never read
	rel := fx.rel
	rel.Assets = rel.Assets[:3]
	target := writeTarget(t, contentOld)
	inst := newTestInstaller(target, io.Discard)
	inst.SkipChecksum = true
	staged, err := inst.Prepare(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = staged.Close() }()
	for _, p := range fx.requested() {
		if p == checksumsPath {
			t.Fatal("checksums must not be fetched when SkipChecksum is set")
		}
	}
	if err := staged.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, target); got != newBinary {
		t.Fatalf("target: %q", got)
	}
}

func TestArchiveInstaller_MissingPlatformAssetMakesNoRequest(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	rel := Release{Tag: fx.rel.Tag, Assets: []Asset{fx.rel.Assets[0], fx.rel.Assets[1], fx.rel.Assets[3]}}
	target := writeTarget(t, contentOld)
	_, err := newTestInstaller(target, io.Discard).Prepare(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), fx.assetName) || !strings.Contains(err.Error(), "available:") {
		t.Fatalf("got %v", err)
	}
	if got := fx.requested(); len(got) != 0 {
		t.Fatalf("no request may be made before an asset is selected: %v", got)
	}
}

func TestArchiveInstaller_ManagedTargetRefused(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	target := writeTarget(t, contentOld)
	inst := newTestInstaller(target, io.Discard)
	inst.Managed = []ManagedInstall{{
		Pattern: regexp.MustCompile(regexp.QuoteMeta(filepath.Dir(target))),
		Manager: "testpm",
		Hint:    "testpm upgrade {name}",
	}}
	_, err := inst.Prepare(context.Background(), fx.rel)
	if !errors.Is(err, ErrManagedInstall) || !strings.Contains(err.Error(), "testpm upgrade tool") {
		t.Fatalf("got %v", err)
	}
	if got := fx.requested(); len(got) != 0 {
		t.Fatalf("managed check must run before any request: %v", got)
	}
}

func TestArchiveInstaller_UnwritableDirMakesNoRequest(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	fx := newArchiveFixture(t, newBinary, false)
	target := writeTarget(t, contentOld)
	dir := filepath.Dir(target)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	_, err := newTestInstaller(target, io.Discard).Prepare(context.Background(), fx.rel)
	if !errors.Is(err, ErrTargetNotWritable) {
		t.Fatalf("got %v", err)
	}
	if got := fx.requested(); len(got) != 0 {
		t.Fatalf("writable check must run before any request: %v", got)
	}
}

func TestArchiveInstaller_ResolvesSymlinkAndDefaultsName(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	realPath := writeTarget(t, contentOld)
	link := filepath.Join(t.TempDir(), "tool-link")
	if err := os.Symlink(realPath, link); err != nil {
		t.Fatal(err)
	}
	inst := &ArchiveInstaller{TargetPath: link, Out: io.Discard, Managed: []ManagedInstall{}}
	staged, err := inst.Prepare(context.Background(), fx.rel)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = staged.Close() }()
	if err := staged.Commit(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := readTarget(t, realPath); got != newBinary {
		t.Fatalf("real file: %q", got)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the symlink must survive; only its target is replaced")
	}
}

func TestArchiveInstaller_Non200IsError(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	assets := append([]Asset(nil), fx.rel.Assets...)
	assets[2].URL = fx.srv.URL + "/missing.tar.gz"
	rel := Release{Tag: fx.rel.Tag, Assets: assets}
	target := writeTarget(t, contentOld)
	_, err := newTestInstaller(target, io.Discard).Prepare(context.Background(), rel)
	if err == nil || !strings.Contains(err.Error(), "status 404") {
		t.Fatalf("got %v", err)
	}
}

func TestArchiveInstaller_AssetName(t *testing.T) {
	inst := &ArchiveInstaller{}
	want := fmt.Sprintf("tool_1.2.3_%s_%s.tar.gz", runtime.GOOS, runtime.GOARCH)
	if got := inst.assetName(testRepo, tagV123); got != want {
		t.Fatalf("default template: got %q, want %q", got, want)
	}
	inst.AssetTemplate = "{name}-{os}-{arch}-v{version}.tgz"
	want = fmt.Sprintf("tool-%s-%s-v1.2.3.tgz", runtime.GOOS, runtime.GOARCH)
	if got := inst.assetName(testRepo, tagV123); got != want {
		t.Fatalf("custom template: got %q, want %q", got, want)
	}
}

func TestArchiveInstaller_DefaultClientHeaderTimeout(t *testing.T) {
	inst := &ArchiveInstaller{}
	tr, ok := inst.client().Transport.(*http.Transport)
	if !ok || tr.ResponseHeaderTimeout != responseHeaderTimeout {
		t.Fatalf("default transport: %+v", inst.client().Transport)
	}
	custom := &http.Client{}
	inst = &ArchiveInstaller{Client: custom}
	if inst.client() != custom {
		t.Fatal("an explicit Client must be used as is")
	}
}

// TestArchiveInstaller_ClientIsConcurrencySafe runs under -race: a shared
// installer must not be mutated by client().
func TestArchiveInstaller_ClientIsConcurrencySafe(t *testing.T) {
	inst := &ArchiveInstaller{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() { _ = inst.client() })
	}
	wg.Wait()
	if inst.Client != nil {
		t.Fatal("client() must leave the exported field untouched")
	}
}

func TestArchiveInstaller_DownloadSizeMismatchIsRejected(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	declared := fx.rel.Assets[2].Size
	cases := []struct {
		name string
		size int64
	}{
		{"body longer than listed", declared - 1},
		{"body shorter than listed", declared + 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assets := append([]Asset(nil), fx.rel.Assets...)
			assets[2].Size = tc.size
			rel := Release{Tag: fx.rel.Tag, Assets: assets}
			target := writeTarget(t, contentOld)
			staged, err := newTestInstaller(target, io.Discard).Prepare(context.Background(), rel)
			if err == nil || !strings.Contains(err.Error(), "release lists") {
				t.Fatalf("got %v", err)
			}
			if staged != nil {
				t.Fatal("no staged update may be returned on failure")
			}
			if got := readTarget(t, target); got != contentOld {
				t.Fatalf("target changed: %q", got)
			}
		})
	}
	for _, path := range fx.requested() {
		if path == checksumsPath {
			t.Fatal("the size check must fail before the checksum file is fetched")
		}
	}
}

func TestArchiveInstaller_UnknownSizeIsUnbounded(t *testing.T) {
	fx := newArchiveFixture(t, newBinary, false)
	assets := append([]Asset(nil), fx.rel.Assets...)
	assets[2].Size = 0
	rel := Release{Tag: fx.rel.Tag, Assets: assets}
	target := writeTarget(t, contentOld)
	staged, err := newTestInstaller(target, io.Discard).Prepare(context.Background(), rel)
	if err != nil {
		t.Fatal(err)
	}
	_ = staged.Close()
}
