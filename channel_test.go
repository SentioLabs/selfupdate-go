// Package selfupdate's own test package is used (not selfupdate_test) per
// the task spec, so these tests can be extended later to exercise unexported
// helpers without a second test package.
//
//nolint:testpackage // internal package per task spec envctl-04qa.00bow5.1.1.4
package selfupdate

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// tagV0110RC3 and tagV0100 name literals repeated across the fixtures below
// so their reuse doesn't trip goconst.
const (
	tagV0110RC3 = "v0.11.0-rc3"
	tagV0100    = "v0.10.0"
)

// tagV0200RC101, tagV0190 and tagV0210 back the overflow-page fixture below.
const (
	tagV0200RC101 = "v0.20.0-rc.101"
	tagV0190      = "v0.19.0"
	tagV0210      = "v0.21.0"
)

// fakeSource serves canned releases. latestErr and listErr force failures.
type fakeSource struct {
	latest      Release
	list        []Release
	latestErr   error
	listErr     error
	listCalls   int
	latestCalls int
}

func (f *fakeSource) Latest(context.Context) (Release, error) {
	f.latestCalls++
	return f.latest, f.latestErr
}

func (f *fakeSource) List(context.Context, int) ([]Release, error) {
	f.listCalls++
	return f.list, f.listErr
}

// manyRCReleases returns 101 rc prereleases, newest first, from
// v0.20.0-rc.101 down to v0.20.0-rc.1. That's one more than maxPerPage, so a
// single List page can't also contain a stable release.
func manyRCReleases() []Release {
	releases := make([]Release, 101)
	for i := range releases {
		releases[i] = Release{Tag: fmt.Sprintf("v0.20.0-rc.%d", 101-i), Prerelease: true}
	}
	return releases
}

// fixtureReleases mirrors arc's fixture: undotted and dotted rc tags,
// nightlies of the next version, one stable.
func fixtureReleases() []Release {
	return []Release{
		{Tag: tagV0110RC3, Prerelease: true},
		{Tag: "v0.11.0-rc2", Prerelease: true},
		{Tag: "v0.11.0-rc.1", Prerelease: true},
		{Tag: "v0.11.0-nightly.20260302", Prerelease: true},
		{Tag: "v0.11.0-nightly.20260301", Prerelease: true},
		{Tag: tagV0100},
	}
}

func TestResolve_StableUsesLatest(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV0100}, list: fixtureReleases()}
	for _, ch := range []Channel{ChannelStable, ""} {
		rel, err := Resolve(context.Background(), src, ch, DefaultChannels)
		if err != nil || rel.Tag != tagV0100 {
			t.Fatalf("channel %q: got %q, %v", ch, rel.Tag, err)
		}
	}
	if src.listCalls != 0 {
		t.Fatal("stable must not list releases")
	}
}

func TestResolve_RC(t *testing.T) {
	src := &fakeSource{list: fixtureReleases()}
	rel, err := Resolve(context.Background(), src, ChannelRC, DefaultChannels)
	if err != nil || rel.Tag != tagV0110RC3 {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
}

func TestResolve_RCStableIsNewer(t *testing.T) {
	src := &fakeSource{list: []Release{
		{Tag: "v0.12.0"},
		{Tag: tagV0110RC3, Prerelease: true},
		{Tag: tagV0100},
	}}
	rel, err := Resolve(context.Background(), src, ChannelRC, DefaultChannels)
	if err != nil || rel.Tag != "v0.12.0" {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
}

func TestResolve_Nightly(t *testing.T) {
	src := &fakeSource{list: fixtureReleases()}
	rel, err := Resolve(context.Background(), src, ChannelNightly, DefaultChannels)
	if err != nil || rel.Tag != "v0.11.0-nightly.20260302" {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
}

func TestResolve_NightlyStableIsNewer(t *testing.T) {
	src := &fakeSource{list: []Release{
		{Tag: "v0.11.0"},
		{Tag: "v0.10.0-nightly.20260302", Prerelease: true},
	}}
	rel, err := Resolve(context.Background(), src, ChannelNightly, DefaultChannels)
	if err != nil || rel.Tag != "v0.11.0" {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
}

func TestResolve_NoChannelMatchFallsBackToStable(t *testing.T) {
	src := &fakeSource{list: []Release{{Tag: tagV0100}}}
	rel, err := Resolve(context.Background(), src, ChannelNightly, DefaultChannels)
	if err != nil || rel.Tag != tagV0100 {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
}

func TestResolve_NothingFound(t *testing.T) {
	src := &fakeSource{}
	_, err := Resolve(context.Background(), src, ChannelRC, DefaultChannels)
	if err == nil || !strings.Contains(err.Error(), "no rc release found") {
		t.Fatalf("got %v", err)
	}
}

func TestResolve_UnknownChannel(t *testing.T) {
	_, err := Resolve(context.Background(), &fakeSource{}, "beta", DefaultChannels)
	if err == nil || !strings.Contains(err.Error(), "unknown channel") {
		t.Fatalf("got %v", err)
	}
}

func TestResolve_NilSpecsMeansDefaults(t *testing.T) {
	src := &fakeSource{list: fixtureReleases()}
	rel, err := Resolve(context.Background(), src, ChannelRC, nil)
	if err != nil || rel.Tag != tagV0110RC3 {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
}

func TestResolve_SourceErrorsPropagate(t *testing.T) {
	boom := errors.New("boom")
	stableSrc := &fakeSource{latestErr: boom}
	if _, err := Resolve(context.Background(), stableSrc, ChannelStable, nil); !errors.Is(err, boom) {
		t.Fatalf("stable: %v", err)
	}
	if _, err := Resolve(context.Background(), &fakeSource{listErr: boom}, ChannelRC, nil); !errors.Is(err, boom) {
		t.Fatalf("rc: %v", err)
	}
}

func TestResolve_OverflowPageFallsBackToLatest(t *testing.T) {
	src := &fakeSource{list: manyRCReleases(), latest: Release{Tag: tagV0190}}
	rel, err := Resolve(context.Background(), src, ChannelRC, DefaultChannels)
	if err != nil || rel.Tag != tagV0200RC101 {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
	if src.latestCalls != 1 {
		t.Fatalf("latestCalls = %d, want 1", src.latestCalls)
	}
}

func TestResolve_OverflowPageStableNewerViaLatest(t *testing.T) {
	src := &fakeSource{list: manyRCReleases(), latest: Release{Tag: tagV0210}}
	rel, err := Resolve(context.Background(), src, ChannelRC, DefaultChannels)
	if err != nil || rel.Tag != tagV0210 {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
	if src.latestCalls != 1 {
		t.Fatalf("latestCalls = %d, want 1", src.latestCalls)
	}
}

func TestResolve_OverflowPageLatestErrors(t *testing.T) {
	boom := errors.New("boom")
	src := &fakeSource{list: manyRCReleases(), latestErr: boom}
	if _, err := Resolve(context.Background(), src, ChannelRC, DefaultChannels); !errors.Is(err, boom) {
		t.Fatalf("got %v", err)
	}
}

func TestResolve_OverflowPageIgnoresPrereleaseLatest(t *testing.T) {
	src := &fakeSource{
		list:   manyRCReleases(),
		latest: Release{Tag: "v0.21.0-rc.1", Prerelease: true},
	}
	rel, err := Resolve(context.Background(), src, ChannelRC, DefaultChannels)
	if err != nil || rel.Tag != tagV0200RC101 {
		t.Fatalf("got %q, %v", rel.Tag, err)
	}
	if src.latestCalls != 1 {
		t.Fatalf("latestCalls = %d, want 1", src.latestCalls)
	}
}

func TestResolve_ReturnsAssets(t *testing.T) {
	asset := Asset{Name: "tool.tar.gz", URL: "https://dl.example/tool.tar.gz", Size: 1}
	src := &fakeSource{list: []Release{
		{Tag: tagV0110RC3, Prerelease: true, Assets: []Asset{asset}},
		{Tag: tagV0100},
	}}
	rel, err := Resolve(context.Background(), src, ChannelRC, nil)
	if err != nil || rel.Tag != tagV0110RC3 || len(rel.Assets) != 1 || rel.Assets[0] != asset {
		t.Fatalf("got %+v, %v", rel, err)
	}
}
