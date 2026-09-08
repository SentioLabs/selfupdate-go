package e2e_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	owner = "acme"
	repo  = "mytool"
	// runTimeout bounds one subprocess invocation.
	runTimeout = 60 * time.Second
)

// ghAsset and ghRelease mirror the JSON fields GitHubSource reads.
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type ghRelease struct {
	TagName    string    `json:"tag_name"`
	Prerelease bool      `json:"prerelease"`
	Draft      bool      `json:"draft"`
	Assets     []ghAsset `json:"assets"`
}

// assetName renders the goreleaser default asset name for the running platform.
func assetName(version string) string {
	return fmt.Sprintf("%s_%s_%s_%s.tar.gz", repo, strings.TrimPrefix(version, "v"), runtime.GOOS, runtime.GOARCH)
}

// releaseServer serves one GitHub-shaped release for owner acme, repo mytool.
type releaseServer struct {
	srv      *httptest.Server
	mu       sync.Mutex
	requests []string
}

// newReleaseServer advertises latest as the newest release. The tarball
// always holds newBinary. When tamper is true the checksum file lists a
// wrong digest for it. Closed via t.Cleanup.
func newReleaseServer(t *testing.T, latest string, tamper bool) *releaseServer {
	t.Helper()
	s := &releaseServer{}
	asset := assetName(latest)
	archive := tarGzOf(t, newBinary)
	sum := sha256.Sum256(archive)
	if tamper {
		sum[0] ^= 0xff
	}
	checksums := hex.EncodeToString(sum[:]) + "  " + asset + "\n"
	release := func() ghRelease {
		return ghRelease{TagName: latest, Assets: []ghAsset{
			{Name: asset, BrowserDownloadURL: s.srv.URL + "/dl/" + asset, Size: int64(len(archive))},
			{Name: "checksums.txt", BrowserDownloadURL: s.srv.URL + "/dl/checksums.txt", Size: int64(len(checksums))},
		}}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.record(r.URL.Path)
		switch r.URL.Path {
		case "/repos/" + owner + "/" + repo + "/releases/latest":
			_ = json.NewEncoder(w).Encode(release())
		case "/repos/" + owner + "/" + repo + "/releases":
			_ = json.NewEncoder(w).Encode([]ghRelease{release()})
		case "/dl/" + asset:
			w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
			_, _ = w.Write(archive)
		case "/dl/checksums.txt":
			_, _ = io.WriteString(w, checksums)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	s.srv = httptest.NewServer(mux)
	t.Cleanup(s.srv.Close)
	return s
}

func (s *releaseServer) record(path string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.requests = append(s.requests, path)
}

// URL is the API base for MYTOOL_GITHUB_API.
func (s *releaseServer) URL() string { return s.srv.URL }

// Requests returns the paths requested so far, in order.
func (s *releaseServer) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.requests...)
}

// dlRequests filters Requests down to /dl/ paths, in order.
func (s *releaseServer) dlRequests() []string {
	var out []string
	for _, p := range s.Requests() {
		if strings.HasPrefix(p, "/dl/") {
			out = append(out, p)
		}
	}
	return out
}

// tarGzOf packs the file at path as a single entry named mytool, mode 0755.
func tarGzOf(t *testing.T, path string) []byte {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	hdr := &tar.Header{Name: repo, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// installOld copies oldBinary to path at mode 0755, creating parent dirs.
//
//nolint:unused // called by the scenarios task built on this harness
func installOld(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(oldBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o755); err != nil { //nolint:gosec // executable fixture
		t.Fatal(err)
	}
}

// runResult is one subprocess invocation.
type runResult struct {
	Stdout string
	Stderr string
	Code   int
}

// run executes bin with args and MYTOOL_GITHUB_API set to api, under a
// 60s timeout. A non-zero exit is returned in Code, not treated as fatal.
func run(t *testing.T, bin, api string, args ...string) runResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), runTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = append(os.Environ(), envAPI+"="+api)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := runResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		res.Code = exitErr.ExitCode()
	default:
		t.Fatalf("run %s %v: %v", bin, args, err)
	}
	return res
}

// versionOf runs bin --version and returns the trimmed output.
func versionOf(t *testing.T, bin string) string {
	t.Helper()
	res := run(t, bin, "", "--version")
	if res.Code != 0 {
		t.Fatalf("%s --version exited %d: %s", bin, res.Code, res.Stderr)
	}
	return strings.TrimSpace(res.Stdout)
}

// getBody fetches url from the fixture server and returns the body.
func getBody(t *testing.T, url string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s: status %d", url, resp.StatusCode)
	}
	return body
}

func TestFixture_BinariesReportTheirVersion(t *testing.T) {
	requireBinaries(t)
	if got := versionOf(t, oldBinary); got != oldVersion {
		t.Fatalf("old binary reports %q", got)
	}
	if got := versionOf(t, newBinary); got != newVersion {
		t.Fatalf("new binary reports %q", got)
	}
}

func TestFixture_ServesReleaseAndChecksums(t *testing.T) {
	requireBinaries(t)
	s := newReleaseServer(t, newVersion, false)
	var rel ghRelease
	if err := json.Unmarshal(getBody(t, s.URL()+"/repos/acme/mytool/releases/latest"), &rel); err != nil {
		t.Fatal(err)
	}
	asset := assetName(newVersion)
	if rel.TagName != newVersion || len(rel.Assets) != 2 || rel.Assets[0].Name != asset {
		t.Fatalf("release: %+v", rel)
	}
	archive := getBody(t, rel.Assets[0].BrowserDownloadURL)
	sums := string(getBody(t, rel.Assets[1].BrowserDownloadURL))
	sum := sha256.Sum256(archive)
	if want := hex.EncodeToString(sum[:]) + "  " + asset + "\n"; sums != want {
		t.Fatalf("checksums.txt:\n%s\nwant:\n%s", sums, want)
	}
	if got := s.dlRequests(); len(got) != 2 || got[0] != "/dl/"+asset || got[1] != "/dl/checksums.txt" {
		t.Fatalf("dl requests: %v", got)
	}
}

func TestFixture_TamperFlipsChecksum(t *testing.T) {
	requireBinaries(t)
	s := newReleaseServer(t, newVersion, true)
	asset := assetName(newVersion)
	archive := getBody(t, s.URL()+"/dl/"+asset)
	sums := string(getBody(t, s.URL()+"/dl/checksums.txt"))
	sum := sha256.Sum256(archive)
	if strings.HasPrefix(sums, hex.EncodeToString(sum[:])) {
		t.Fatal("tampered checksum must not match the archive")
	}
}
