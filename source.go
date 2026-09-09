package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Asset is one downloadable file attached to a release.
type Asset struct {
	Name string
	URL  string // browser_download_url
	Size int64
}

// Release is the subset of a forge release the library needs.
type Release struct {
	Tag        string
	Prerelease bool
	Assets     []Asset
}

// Source lists releases. Implementations must return List newest first.
type Source interface {
	Latest(ctx context.Context) (Release, error)
	List(ctx context.Context, limit int) ([]Release, error)
}

// defaultGitHubAPI is the public GitHub API base.
const defaultGitHubAPI = "https://api.github.com"

// maxPerPage is GitHub's page-size ceiling for the releases list.
const maxPerPage = 100

// GitHubSource reads github.com/<Owner>/<Repo>/releases.
type GitHubSource struct {
	Owner   string
	Repo    string
	BaseURL string       // default https://api.github.com; tests point it at httptest
	Token   string       // optional; sent as "Authorization: Bearer <Token>"
	Client  *http.Client // default http.DefaultClient
}

// githubAsset is the subset of GitHub's asset JSON the library reads.
type githubAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// githubRelease is the subset of GitHub's release JSON the library reads.
type githubRelease struct {
	TagName    string        `json:"tag_name"`
	Prerelease bool          `json:"prerelease"`
	Draft      bool          `json:"draft"`
	Assets     []githubAsset `json:"assets"`
}

// release converts the GitHub payload into the library's Release.
func (r githubRelease) release() Release {
	assets := make([]Asset, 0, len(r.Assets))
	for _, a := range r.Assets {
		assets = append(assets, Asset{Name: a.Name, URL: a.BrowserDownloadURL, Size: a.Size})
	}
	return Release{Tag: r.TagName, Prerelease: r.Prerelease, Assets: assets}
}

// Latest returns the release GitHub marks as latest (never a prerelease).
func (g *GitHubSource) Latest(ctx context.Context) (Release, error) {
	var rel githubRelease
	if err := g.get(ctx, "/releases/latest", &rel); err != nil {
		return Release{}, err
	}
	return rel.release(), nil
}

// List returns up to limit releases, newest first, without drafts. A limit
// outside 1..100 is clamped to 100.
func (g *GitHubSource) List(ctx context.Context, limit int) ([]Release, error) {
	if limit <= 0 || limit > maxPerPage {
		limit = maxPerPage
	}
	var raw []githubRelease
	if err := g.get(ctx, "/releases?per_page="+strconv.Itoa(limit), &raw); err != nil {
		return nil, err
	}
	out := make([]Release, 0, len(raw))
	for _, r := range raw {
		if r.Draft {
			continue
		}
		out = append(out, r.release())
	}
	return out, nil
}

func (g *GitHubSource) get(ctx context.Context, path string, out any) error {
	base := g.BaseURL
	if base == "" {
		base = defaultGitHubAPI
	}
	url := strings.TrimRight(base, "/") + "/repos/" + g.Owner + "/" + g.Repo + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}

	client := g.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("github: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("github: %s returned status %d", path, resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("github: decode %s: %w", path, err)
	}
	return nil
}
