// Package selfupdate's own test package is used (not selfupdate_test) per
// the task spec, so these tests can be extended later to exercise unexported
// helpers without a second test package.
//
//nolint:testpackage // internal package per task spec envctl-04qa.00bow5.1.1.3
package selfupdate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// testOwner, testRepo and the tag constants name literals repeated across the
// fixtures below so their reuse doesn't trip goconst.
const (
	testOwner = "acme"
	testRepo  = "tool"
	tagV123   = "v1.2.3"
	tagV131rc = "v1.3.0-rc.1"
	tagV100   = "v1.0.0"
)

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type ghFixture struct {
	TagName    string    `json:"tag_name"`
	Prerelease bool      `json:"prerelease"`
	Draft      bool      `json:"draft"`
	Assets     []ghAsset `json:"assets"`
}

func newGitHubTestServer(t *testing.T, latest ghFixture, list []ghFixture, wantAuth string) *httptest.Server {
	t.Helper()
	latestPath := "/repos/" + testOwner + "/" + testRepo + "/releases/latest"
	listPath := "/repos/" + testOwner + "/" + testRepo + "/releases"
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if wantAuth != "" && r.Header.Get("Authorization") != wantAuth {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case latestPath:
			_ = json.NewEncoder(w).Encode(latest)
		case listPath:
			if r.URL.Query().Get("per_page") == "" {
				t.Errorf("per_page missing: %s", r.URL.RawQuery)
			}
			_ = json.NewEncoder(w).Encode(list)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
}

func TestGitHubSource_Latest(t *testing.T) {
	srv := newGitHubTestServer(t, ghFixture{TagName: tagV123}, nil, "")
	defer srv.Close()
	src := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL}

	rel, err := src.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if rel.Tag != tagV123 || rel.Prerelease {
		t.Fatalf("got %+v", rel)
	}
}

func TestGitHubSource_ListDropsDraftsKeepsOrder(t *testing.T) {
	list := []ghFixture{
		{TagName: tagV131rc, Prerelease: true},
		{TagName: "v1.2.4", Draft: true},
		{TagName: tagV123},
	}
	srv := newGitHubTestServer(t, ghFixture{}, list, "")
	defer srv.Close()
	src := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL}

	const wantLimit = 50
	got, err := src.List(context.Background(), wantLimit)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Tag != tagV131rc || !got[0].Prerelease || got[1].Tag != tagV123 {
		t.Fatalf("got %+v", got)
	}
}

func TestGitHubSource_TokenHeader(t *testing.T) {
	srv := newGitHubTestServer(t, ghFixture{TagName: tagV100}, nil, "Bearer secret")
	defer srv.Close()

	withToken := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL, Token: "secret"}
	if _, err := withToken.Latest(context.Background()); err != nil {
		t.Fatalf("with token: %v", err)
	}
	without := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL}
	if _, err := without.Latest(context.Background()); err == nil {
		t.Fatal("expected 401 error without token")
	}
}

func TestGitHubSource_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()
	src := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL}
	if _, err := src.Latest(context.Background()); err == nil {
		t.Fatal("expected error")
	}
	const limit = 10
	if _, err := src.List(context.Background(), limit); err == nil {
		t.Fatal("expected error")
	}
}

func TestGitHubSource_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	src := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL}
	if _, err := src.Latest(context.Background()); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestGitHubSource_ContextCancelled(t *testing.T) {
	srv := newGitHubTestServer(t, ghFixture{TagName: tagV100}, nil, "")
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL}
	if _, err := src.Latest(ctx); err == nil {
		t.Fatal("expected context error")
	}
}

func TestGitHubSource_DecodesAssets(t *testing.T) {
	const tarball = "tool_1.2.3_linux_amd64.tar.gz"
	const tarballSize, sumsSize, limit = 1234, 99, 10
	latest := ghFixture{TagName: tagV123, Assets: []ghAsset{
		{Name: tarball, BrowserDownloadURL: "https://dl.example/" + tarball, Size: tarballSize},
		{Name: "checksums.txt", BrowserDownloadURL: "https://dl.example/checksums.txt", Size: sumsSize},
	}}
	srv := newGitHubTestServer(t, latest, []ghFixture{latest}, "")
	defer srv.Close()
	src := &GitHubSource{Owner: testOwner, Repo: testRepo, BaseURL: srv.URL}

	rel, err := src.Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := Asset{Name: tarball, URL: "https://dl.example/" + tarball, Size: tarballSize}
	if len(rel.Assets) != 2 || rel.Assets[0] != want {
		t.Fatalf("Latest assets: got %+v, want first %+v", rel.Assets, want)
	}

	list, err := src.List(context.Background(), limit)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || len(list[0].Assets) != 2 || list[0].Assets[1].Name != "checksums.txt" {
		t.Fatalf("List assets: %+v", list)
	}
}
