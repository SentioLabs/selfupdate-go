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
	"io"
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

// toolName is the name of the binary and GitHub repo.
const toolName = "mytool"

// exitStartup is the exit code when the executable path cannot be resolved.
const exitStartup = 2

// exitFailure is the exit code for general execution failures.
const exitFailure = 1

func main() {
	exe, err := os.Executable()
	if err != nil {
		_, _ = fmt.Fprintln(os.Stderr, toolName+": locate executable:", err)
		os.Exit(exitStartup)
	}
	root := &cobra.Command{
		Use:     toolName,
		Short:   "Demo CLI for selfupdate-go",
		Version: version,
	}
	root.SetVersionTemplate("{{.Version}}\n")
	root.AddCommand(cobracmd.New(newUpdater(root.OutOrStdout(), exe)))
	if err := root.Execute(); err != nil {
		os.Exit(exitFailure)
	}
}

// newUpdater mirrors the README wiring. Every ArchiveInstaller field is
// left at its default so asset selection, target resolution and the
// managed-path list are production behaviour.
func newUpdater(w io.Writer, exe string) *selfupdate.Updater {
	return &selfupdate.Updater{
		Name:    toolName,
		Version: version,
		Source:  &selfupdate.GitHubSource{Owner: "acme", Repo: toolName, BaseURL: os.Getenv(envAPI)},
		// MemStore keeps the channel for this process only; a real CLI persists it.
		Store:       &selfupdate.MemStore{},
		Installer:   &selfupdate.ArchiveInstaller{},
		PreInstall:  reportHook(w, exe, "pre-install"),
		PostInstall: reportHook(w, exe, "post-install"),
	}
}

// reportHook returns a hook that runs exe --version and prints what the
// binary on disk reports. exe is resolved once at startup on purpose: after
// the update, os.Executable() in this process points at the replaced file's
// old inode, not the new binary.
func reportHook(w io.Writer, exe, name string) func(context.Context, string, string) error {
	return func(ctx context.Context, _, _ string) error {
		out, err := exec.CommandContext(ctx, exe, "--version").Output()
		if err != nil {
			// A failing hook aborts the update on purpose; the e2e suite relies on these lines.
			return fmt.Errorf("%s: run %s --version: %w", name, exe, err)
		}
		_, _ = fmt.Fprintf(w, "%s: binary reports %s\n", name, strings.TrimSpace(string(out)))
		return nil
	}
}
