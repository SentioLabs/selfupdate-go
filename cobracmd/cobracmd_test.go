package cobracmd_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/sentiolabs/selfupdate-go"
	"github.com/sentiolabs/selfupdate-go/cobracmd"
	"github.com/spf13/cobra"
)

const toolName = "tool"

type fakeSource struct{ latest, rc string }

func (f fakeSource) Latest(context.Context) (selfupdate.Release, error) {
	return selfupdate.Release{Tag: f.latest}, nil
}

func (f fakeSource) List(context.Context, int) ([]selfupdate.Release, error) {
	return []selfupdate.Release{{Tag: f.rc, Prerelease: true}, {Tag: f.latest}}, nil
}

func newUpdater(store selfupdate.Store) (*selfupdate.Updater, *bytes.Buffer) {
	out := &bytes.Buffer{}
	return &selfupdate.Updater{
		Name: toolName, Version: "v1.0.0",
		Source: fakeSource{latest: "v2.0.0", rc: "v2.1.0-rc.1"},
		Store:  store, Out: out, ErrOut: out, In: strings.NewReader(""),
	}, out
}

func run(t *testing.T, root *cobra.Command, args ...string) error {
	t.Helper()
	root.SetArgs(args)
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	return root.Execute()
}

func TestTree(t *testing.T) {
	u, _ := newUpdater(&selfupdate.MemStore{})
	self := cobracmd.New(u)
	if self.Use != "self" {
		t.Fatalf("Use = %q", self.Use)
	}
	names := map[string]bool{}
	for _, c := range self.Commands() {
		names[c.Name()] = true
	}
	if !names["update"] || !names["channel"] {
		t.Fatalf("subcommands: %v", names)
	}
	update, _, _ := self.Find([]string{"update"})
	if f := update.Flags().Lookup("check"); f == nil || f.Shorthand != "" {
		t.Fatalf("--check must exist with no shorthand by default: %+v", f)
	}
	if f := update.Flags().Lookup("force"); f == nil || f.Shorthand != "f" {
		t.Fatal("--force/-f missing")
	}
	if f := update.Flags().Lookup("yes"); f == nil || f.Shorthand != "y" {
		t.Fatal("--yes/-y missing on update")
	}
	channel, _, _ := self.Find([]string{"channel"})
	if f := channel.Flags().Lookup("yes"); f == nil || f.Shorthand != "y" {
		t.Fatal("--yes/-y missing on channel")
	}
}

func TestWithCheckShorthand(t *testing.T) {
	u, _ := newUpdater(&selfupdate.MemStore{})
	update := cobracmd.NewUpdate(u, cobracmd.WithCheckShorthand("c"))
	if f := update.Flags().Lookup("check"); f == nil || f.Shorthand != "c" {
		t.Fatalf("shorthand not applied: %+v", f)
	}
}

func TestNoPanicWithPersistentDashC(t *testing.T) {
	// envctl's root command defines -c for --config as a persistent flag.
	root := &cobra.Command{Use: "envctl"}
	var cfg string
	root.PersistentFlags().StringVarP(&cfg, "config", "c", "", "config file")
	u, out := newUpdater(&selfupdate.MemStore{})
	root.AddCommand(cobracmd.New(u))
	if err := run(t, root, "self", "update", "--check"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Update available: v1.0.0 -> v2.0.0") {
		t.Fatalf("output: %s", out.String())
	}
}

func TestUpdateCheckOnRCChannel(t *testing.T) {
	u, out := newUpdater(&selfupdate.MemStore{Current: selfupdate.ChannelRC})
	root := &cobra.Command{Use: toolName}
	root.AddCommand(cobracmd.New(u))
	if err := run(t, root, "self", "update", "--check"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "v2.1.0-rc.1 (rc channel)") {
		t.Fatalf("output: %s", out.String())
	}
}

func TestChannelShowAndSwitch(t *testing.T) {
	store := &selfupdate.MemStore{}
	u, out := newUpdater(store)
	root := &cobra.Command{Use: toolName}
	root.AddCommand(cobracmd.New(u))

	if err := run(t, root, "self", "channel"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Current update channel: stable") {
		t.Fatalf("output: %s", out.String())
	}
	out.Reset()
	if err := run(t, root, "self", "channel", "nightly", "-y"); err != nil {
		t.Fatal(err)
	}
	if store.Current != selfupdate.ChannelNightly || !strings.Contains(out.String(), "Switched to nightly channel") {
		t.Fatalf("store %q output %s", store.Current, out.String())
	}
	if err := run(t, root, "self", "channel", "beta", "-y"); err == nil {
		t.Fatal("invalid channel must be an error")
	}
}
