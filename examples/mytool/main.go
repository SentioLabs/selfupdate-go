// Command mytool is the runnable version of the README example. It wires
// selfupdate-go with cobra, uses ArchiveInstaller with every default, and
// shows PreInstall and PostInstall hooks that report which binary is on disk.
//
// MYTOOL_GITHUB_API overrides the GitHub API base URL, for GitHub Enterprise
// or a local mock. Unset means api.github.com.
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/sentiolabs/selfupdate-go"
	"github.com/sentiolabs/selfupdate-go/cobracmd"
	"github.com/spf13/cobra"
)

// version is set at build time: -ldflags "-X main.version=v1.0.0".
var version = "dev"

// envAPI overrides GitHubSource.BaseURL, for GitHub Enterprise or a local
// mock. Unset means api.github.com.
const envAPI = "MYTOOL_GITHUB_API"

func main() {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "mytool: locate executable:", err)
		os.Exit(2)
	}
	root := &cobra.Command{
		Use:     "mytool",
		Short:   "Demo CLI for selfupdate-go",
		Version: version,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(cobracmd.New(newUpdater(exe)))
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// newUpdater mirrors the README wiring. Every ArchiveInstaller field is
// left at its default so asset selection, target resolution and the
// managed-path list are production behaviour.
func newUpdater(exe string) *selfupdate.Updater {
	return &selfupdate.Updater{
		Name:        "mytool",
		Version:     version,
		Source:      &selfupdate.GitHubSource{Owner: "acme", Repo: "mytool", BaseURL: os.Getenv(envAPI)},
		Store:       &selfupdate.MemStore{},
		Installer:   &selfupdate.ArchiveInstaller{},
		PreInstall:  reportHook(exe, "pre-install"),
		PostInstall: reportHook(exe, "post-install"),
	}
}

// reportHook returns a hook that runs exe --version and prints what the
// binary on disk reports. exe is resolved once at startup on purpose: after
// the update, os.Executable() in this process points at the replaced file's
// old inode, not the new binary.
func reportHook(exe, name string) func(context.Context, string, string) error {
	return func(ctx context.Context, _, _ string) error {
		out, err := exec.CommandContext(ctx, exe, "--version").Output()
		if err != nil {
			return fmt.Errorf("%s: run %s --version: %w", name, exe, err)
		}
		fmt.Printf("%s: binary reports %s\n", name, strings.TrimSpace(string(out)))
		return nil
	}
}
