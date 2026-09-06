// Package selfupdate's own test package is used (not selfupdate_test) so the
// contract assertions below read exactly like consumer code: the explicit
// type on each "var _ T = value" line is the assertion, not incidental style,
// so it is kept even where staticcheck would suggest inferring it.
//
//nolint:staticcheck,testpackage // contract assertions kept verbatim per plan; see design-m1.md
package selfupdate

import (
	"bytes"
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
)

// --- Contract assertions ---
// These verify the design contracts. Do NOT modify without updating the plan.

func TestContracts(t *testing.T) {
	var _ Channel = ChannelStable
	var _ Channel = ChannelRC
	var _ Channel = ChannelNightly

	spec := ChannelSpec{}
	var _ Channel = spec.Name
	var _ *regexp.Regexp = spec.Pattern
	var _ string = spec.Warning

	r := Release{}
	var _ string = r.Tag
	var _ bool = r.Prerelease

	u := Updater{}
	var _ string = u.Name
	var _ string = u.Version
	var _ Source = u.Source
	var _ Store = u.Store
	var _ Installer = u.Installer
	var _ []ChannelSpec = u.Channels

	o := UpdateOptions{}
	var _ bool = o.Force
	var _ bool = o.Yes
	var _ bool = o.Check

	c := CheckResult{}
	var _ int = c.Cmp
	var _ Channel = c.Channel

	if len(DefaultChannels) != 3 {
		t.Fatalf("DefaultChannels: want 3, got %d", len(DefaultChannels))
	}
	if DefaultChannels[0].Pattern != nil {
		t.Fatal("stable must have no pattern")
	}
	if _, ok := LookupChannel(DefaultChannels, ChannelRC); !ok {
		t.Fatal("rc must be a default channel")
	}
	if _, ok := LookupChannel(DefaultChannels, "beta"); ok {
		t.Fatal("beta must not be a default channel")
	}
}

func TestDefaultPatterns(t *testing.T) {
	rc, _ := LookupChannel(DefaultChannels, ChannelRC)
	nightly, _ := LookupChannel(DefaultChannels, ChannelNightly)
	for _, tag := range []string{"v1.2.3-rc.1", "v1.2.3-rc1", "v0.16.0-rc9"} {
		if !rc.Pattern.MatchString(tag) {
			t.Errorf("rc pattern must match %s", tag)
		}
	}
	for _, tag := range []string{"v1.2.3", "v1.2.3-nightly.20260903", "v1.2.3-rc"} {
		if rc.Pattern.MatchString(tag) {
			t.Errorf("rc pattern must not match %s", tag)
		}
	}
	if !nightly.Pattern.MatchString("v0.6.1-nightly.20260904") {
		t.Error("nightly pattern must match a dated tag")
	}
	if nightly.Pattern.MatchString("v0.6.1-nightly.2026") {
		t.Error("nightly pattern requires an 8-digit date")
	}
}

// --- Behavior tests ---

// tagV200 names a literal repeated across the fixtures below so its reuse
// doesn't trip goconst. tagV100 (source_test.go) and tagV0100 (channel_test.go)
// already cover "v1.0.0" and "v0.10.0".
const tagV200 = "v2.0.0"

type recordingInstaller struct {
	tags []string
	err  error
}

func (r *recordingInstaller) Install(_ context.Context, tag string) error {
	r.tags = append(r.tags, tag)
	return r.err
}

func newTestUpdater(current string, src Source, store Store, in string,
) (u *Updater, out, errOut *bytes.Buffer, inst *recordingInstaller) {
	out, errOut = &bytes.Buffer{}, &bytes.Buffer{}
	inst = &recordingInstaller{}
	u = &Updater{
		Name: testRepo, Version: current, Source: src, Store: store, Installer: inst,
		Out: out, ErrOut: errOut, In: strings.NewReader(in),
	}
	return u, out, errOut, inst
}

func TestUpdater_CurrentChannel(t *testing.T) {
	u := &Updater{Name: testRepo}
	ch, err := u.CurrentChannel()
	if err != nil || ch != ChannelStable {
		t.Fatalf("nil store must mean stable, got %q, %v", ch, err)
	}
	u.Store = &MemStore{Current: ChannelRC}
	if ch, _ = u.CurrentChannel(); ch != ChannelRC {
		t.Fatalf("got %q", ch)
	}
	u.Store = FuncStore{Get: func() (Channel, error) { return "", nil }, Set: nil}
	if ch, _ = u.CurrentChannel(); ch != ChannelStable {
		t.Fatalf("empty stored channel must mean stable, got %q", ch)
	}
}

func TestUpdater_Check(t *testing.T) {
	src := &fakeSource{list: fixtureReleases(), latest: Release{Tag: tagV0100}}
	u := &Updater{Name: testRepo, Version: "0.10.0", Source: src, Store: &MemStore{Current: ChannelRC}}
	res, err := u.Check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Current != tagV0100 || res.Latest != tagV0110RC3 || res.Channel != ChannelRC || res.Cmp <= 0 {
		t.Fatalf("got %+v", res)
	}
}

func TestUpdater_LegacyRCComparisonPreservesTags(t *testing.T) {
	for _, latest := range []string{legacyRC10, dottedRC10} {
		t.Run(latest, func(t *testing.T) {
			checkLegacyRCTags(t, latest)
		})
	}
}

func checkLegacyRCTags(t *testing.T, latest string) {
	t.Helper()
	src := &fakeSource{list: []Release{
		{Tag: latest, Prerelease: true},
		{Tag: legacyRC9, Prerelease: true},
		{Tag: tagV0100},
	}}
	u, out, _, inst := newTestUpdater(legacyRC9, src, &MemStore{Current: ChannelRC}, "")
	res, err := u.Check(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if res.Current != legacyRC9 || res.Latest != latest || res.Cmp <= 0 {
		t.Fatalf("Check: got %+v", res)
	}
	var hookCalled bool
	u.PreInstall = func(_ context.Context, current, target string) error {
		hookCalled = true
		if current != legacyRC9 || target != latest {
			t.Errorf("PreInstall: got %q -> %q", current, target)
		}
		return nil
	}
	if err := u.Update(t.Context(), UpdateOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if !hookCalled || len(inst.tags) != 1 || inst.tags[0] != latest {
		t.Fatalf("hook called: %v, installed tags: %v", hookCalled, inst.tags)
	}
	if !strings.Contains(out.String(), legacyRC9+" -> "+latest) {
		t.Fatalf("output did not preserve tags: %s", out.String())
	}
}

func TestUpdater_UpdateCheckOnlyPrintsAndDoesNotInstall(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV200}}
	u, out, _, inst := newTestUpdater(tagV100, src, nil, "")
	if err := u.Update(context.Background(), UpdateOptions{Check: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Update available: v1.0.0 -> v2.0.0 (stable channel)") {
		t.Fatalf("output: %s", out.String())
	}
	if len(inst.tags) != 0 {
		t.Fatal("check must not install")
	}
}

func TestUpdater_UpdateUpToDateInstallsNothing(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV100}}
	u, out, _, inst := newTestUpdater(tagV100, src, nil, "")
	if err := u.Update(context.Background(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "tool v1.0.0 (stable) is up to date") || len(inst.tags) != 0 {
		t.Fatalf("output: %s, installs: %v", out.String(), inst.tags)
	}
}

func TestUpdater_UpdateCurrentNewerInstallsNothing(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV100}}
	u, out, _, inst := newTestUpdater(tagV200, src, nil, "")
	if err := u.Update(context.Background(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "is newer than the latest stable release v1.0.0") || len(inst.tags) != 0 {
		t.Fatalf("output: %s", out.String())
	}
}

func TestUpdater_UpdateYesRunsPreInstallThenInstall(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV200}}
	u, _, _, inst := newTestUpdater(tagV100, src, nil, "")
	var order []string
	u.PreInstall = func(_ context.Context, current, latest string) error {
		order = append(order, "pre:"+current+"->"+latest)
		return nil
	}
	if err := u.Update(context.Background(), UpdateOptions{Yes: true}); err != nil {
		t.Fatal(err)
	}
	if len(inst.tags) != 1 || inst.tags[0] != tagV200 {
		t.Fatalf("installs: %v", inst.tags)
	}
	if len(order) != 1 || order[0] != "pre:v1.0.0->v2.0.0" {
		t.Fatalf("PreInstall not called correctly: %v", order)
	}
}

func TestUpdater_UpdatePreInstallErrorStopsInstall(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV200}}
	u, _, _, inst := newTestUpdater(tagV100, src, nil, "")
	u.PreInstall = func(context.Context, string, string) error { return errors.New("backup failed") }
	if err := u.Update(context.Background(), UpdateOptions{Yes: true}); err == nil {
		t.Fatal("expected error")
	}
	if len(inst.tags) != 0 {
		t.Fatal("install must not run after PreInstall fails")
	}
}

func TestUpdater_UpdatePromptDeclined(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV200}}
	u, out, _, inst := newTestUpdater(tagV100, src, nil, "n\n")
	if err := u.Update(context.Background(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Continue? [y/N]") ||
		!strings.Contains(out.String(), "Update cancelled.") || len(inst.tags) != 0 {
		t.Fatalf("output: %s, installs: %v", out.String(), inst.tags)
	}
}

func TestUpdater_UpdatePromptAccepted(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV200}}
	u, _, _, inst := newTestUpdater(tagV100, src, nil, "y\n")
	if err := u.Update(context.Background(), UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(inst.tags) != 1 {
		t.Fatalf("installs: %v", inst.tags)
	}
}

func TestUpdater_UpdateForceReinstallsWhenUpToDate(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV100}}
	u, _, _, inst := newTestUpdater(tagV100, src, nil, "")
	if err := u.Update(context.Background(), UpdateOptions{Force: true, Yes: true}); err != nil {
		t.Fatal(err)
	}
	if len(inst.tags) != 1 || inst.tags[0] != tagV100 {
		t.Fatalf("installs: %v", inst.tags)
	}
}

func TestUpdater_UpdateNoInstallerSkipsPreInstall(t *testing.T) {
	src := &fakeSource{latest: Release{Tag: tagV200}}
	var called bool
	u := &Updater{
		Name: testRepo, Version: tagV100, Source: src, Out: &bytes.Buffer{},
		PreInstall: func(context.Context, string, string) error {
			called = true
			return nil
		},
	}
	err := u.Update(context.Background(), UpdateOptions{Yes: true})
	if !errors.Is(err, ErrNoInstaller) {
		t.Fatalf("got %v", err)
	}
	if called {
		t.Fatal("PreInstall must not run when Installer is nil")
	}
}

func TestUpdater_UpdateRCChannelUsesResolve(t *testing.T) {
	src := &fakeSource{list: fixtureReleases(), latest: Release{Tag: tagV0100}}
	u, out, _, _ := newTestUpdater(tagV0100, src, &MemStore{Current: ChannelRC}, "")
	if err := u.Update(context.Background(), UpdateOptions{Check: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v0.11.0-rc3 (rc channel)") {
		t.Fatalf("output: %s", out.String())
	}
}

func TestUpdater_UpdateSourceErrorWrapped(t *testing.T) {
	src := &fakeSource{latestErr: errors.New("offline")}
	u := &Updater{Name: testRepo, Version: tagV100, Source: src}
	err := u.Update(context.Background(), UpdateOptions{Check: true})
	if err == nil || !strings.Contains(err.Error(), "check for updates") {
		t.Fatalf("got %v", err)
	}
}

func TestUpdater_SwitchChannel(t *testing.T) {
	store := &MemStore{}
	u, out, errOut, _ := newTestUpdater(tagV100, &fakeSource{}, store, "y\n")
	if err := u.SwitchChannel(ChannelRC, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(errOut.String(), "Release candidates may contain bugs") {
		t.Fatalf("warning missing: %s", errOut.String())
	}
	if !strings.Contains(out.String(), "Switched to rc channel") ||
		!strings.Contains(out.String(), "Run 'tool self update' to get the latest rc build") {
		t.Fatalf("output: %s", out.String())
	}
	if store.Current != ChannelRC {
		t.Fatalf("store not updated: %q", store.Current)
	}
}

func TestUpdater_SwitchChannelDeclined(t *testing.T) {
	store := &MemStore{}
	u, out, _, _ := newTestUpdater(tagV100, &fakeSource{}, store, "n\n")
	if err := u.SwitchChannel(ChannelNightly, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Cancelled.") || store.Current != "" {
		t.Fatalf("output: %s, store: %q", out.String(), store.Current)
	}
}

func TestUpdater_SwitchChannelYesSkipsPrompt(t *testing.T) {
	store := &MemStore{}
	u, _, errOut, _ := newTestUpdater(tagV100, &fakeSource{}, store, "")
	if err := u.SwitchChannel(ChannelNightly, true); err != nil {
		t.Fatal(err)
	}
	if store.Current != ChannelNightly {
		t.Fatalf("store: %q", store.Current)
	}
	if strings.Contains(errOut.String(), "[y/N]") {
		t.Fatal("yes must skip the prompt")
	}
}

func TestUpdater_SwitchChannelStableNoPrompt(t *testing.T) {
	store := &MemStore{Current: ChannelRC}
	u, out, _, _ := newTestUpdater(tagV100, &fakeSource{}, store, "")
	if err := u.SwitchChannel(ChannelStable, false); err != nil {
		t.Fatal(err)
	}
	if store.Current != ChannelStable || strings.Contains(out.String(), "self update") {
		t.Fatalf("store %q, output %s", store.Current, out.String())
	}
}

func TestUpdater_SwitchChannelInvalid(t *testing.T) {
	u := &Updater{Name: testRepo}
	err := u.SwitchChannel("beta", true)
	if err == nil || !strings.Contains(err.Error(), `invalid channel "beta": must be one of stable, rc, nightly`) {
		t.Fatalf("got %v", err)
	}
}

func TestUpdater_SwitchChannelNoStore(t *testing.T) {
	u := &Updater{Name: testRepo}
	if err := u.SwitchChannel(ChannelRC, true); err == nil {
		t.Fatal("nil store must be an error")
	}
}
