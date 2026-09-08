package selfupdate

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// ErrNoInstaller is returned by Update when Installer is nil.
var ErrNoInstaller = errors.New("selfupdate: no installer configured")

// Updater ties a CLI's identity to a Source, Store and Installer.
type Updater struct {
	Name      string        // binary name used in messages, e.g. "envctl"
	Version   string        // running version: "v1.2.3", "1.2.3" or "dev"
	Source    Source        // where releases are listed
	Store     Store         // where the channel is persisted; nil means stable only
	Installer Installer     // how the binary is replaced
	Channels  []ChannelSpec // nil means DefaultChannels
	Out       io.Writer     // nil means os.Stdout
	ErrOut    io.Writer     // nil means os.Stderr
	In        io.Reader     // nil means os.Stdin
	// PreInstall runs after the user confirms and after Installer.Prepare has
	// downloaded and verified the release, immediately before Staged.Commit.
	PreInstall func(ctx context.Context, current, latest string) error
	// PostInstall runs after Staged.Commit succeeds. Its error is reported
	// with a note that the binary was already replaced.
	PostInstall func(ctx context.Context, current, latest string) error
}

// UpdateOptions controls Update.
type UpdateOptions struct {
	Force bool // install even when up to date or newer
	Yes   bool // skip the confirmation prompt
	Check bool // print status only
}

// CheckResult is what Check found.
type CheckResult struct {
	Current string
	Latest  string // Release.Tag, kept for callers that only print
	Release Release
	Channel Channel
	Cmp     int // Compare(Current, Latest): 0 same, >0 update available, <0 current is newer
}

func (u *Updater) channels() []ChannelSpec {
	if u.Channels == nil {
		return DefaultChannels
	}
	return u.Channels
}

func (u *Updater) out() io.Writer {
	if u.Out == nil {
		return os.Stdout
	}
	return u.Out
}

func (u *Updater) errOut() io.Writer {
	if u.ErrOut == nil {
		return os.Stderr
	}
	return u.ErrOut
}

func (u *Updater) in() io.Reader {
	if u.In == nil {
		return os.Stdin
	}
	return u.In
}

// CurrentChannel returns the persisted channel, or stable when no Store is
// configured or the stored value is empty.
func (u *Updater) CurrentChannel() (Channel, error) {
	if u.Store == nil {
		return ChannelStable, nil
	}
	ch, err := u.Store.Channel()
	if err != nil {
		return "", fmt.Errorf("read update channel: %w", err)
	}
	if ch == "" {
		return ChannelStable, nil
	}
	return ch, nil
}

// Check resolves the latest release for the current channel without installing.
func (u *Updater) Check(ctx context.Context) (CheckResult, error) {
	ch, err := u.CurrentChannel()
	if err != nil {
		return CheckResult{}, err
	}
	rel, err := Resolve(ctx, u.Source, ch, u.channels())
	if err != nil {
		return CheckResult{}, err
	}
	return CheckResult{
		Current: NormalizeVersion(u.Version),
		Latest:  rel.Tag,
		Release: rel,
		Channel: ch,
		Cmp:     Compare(u.Version, rel.Tag),
	}, nil
}

// Update checks for a newer release on the current channel and, unless
// opts.Check, installs it after confirmation. The installer prepares the
// release first, then PreInstall runs, then the staged update is committed,
// then PostInstall runs.
//
//nolint:revive // CLI output writes always succeed
func (u *Updater) Update(ctx context.Context, opts UpdateOptions) error {
	res, err := u.Check(ctx)
	if err != nil {
		return fmt.Errorf("check for updates: %w", err)
	}
	w := u.out()

	if opts.Check {
		switch {
		case res.Cmp == 0:
			fmt.Fprintf(w, "%s %s (%s) is up to date\n", u.Name, res.Current, res.Channel)
		case res.Cmp > 0:
			fmt.Fprintf(w, "Update available: %s -> %s (%s channel)\n", res.Current, res.Latest, res.Channel)
			fmt.Fprintf(w, "Run '%s self update' to upgrade\n", u.Name)
		default:
			fmt.Fprintf(w, "%s %s is newer than the latest %s release %s\n", u.Name, res.Current, res.Channel, res.Latest)
		}
		return nil
	}

	if res.Cmp == 0 && !opts.Force {
		fmt.Fprintf(w, "%s %s (%s) is up to date\n", u.Name, res.Current, res.Channel)
		return nil
	}
	if res.Cmp < 0 && !opts.Force {
		fmt.Fprintf(w, "%s %s is newer than the latest %s release %s\n", u.Name, res.Current, res.Channel, res.Latest)
		return nil
	}

	fmt.Fprintf(w, "Updating %s %s -> %s...\n", u.Name, res.Current, res.Latest)
	if !opts.Yes && !u.confirm("Continue? [y/N] ") {
		fmt.Fprintln(w, "Update cancelled.")
		return nil
	}
	if u.Installer == nil {
		return ErrNoInstaller
	}
	return u.install(ctx, res)
}

// install runs Prepare, PreInstall, Commit and PostInstall in that order.
// Each step runs only when every earlier step succeeded.
func (u *Updater) install(ctx context.Context, res CheckResult) error {
	staged, err := u.Installer.Prepare(ctx, res.Release)
	if err != nil {
		return fmt.Errorf("prepare update: %w", err)
	}
	defer func() { _ = staged.Close() }()
	if u.PreInstall != nil {
		if err := u.PreInstall(ctx, res.Current, res.Latest); err != nil {
			return fmt.Errorf("pre-install step failed: %w", err)
		}
	}
	if err := staged.Commit(ctx); err != nil {
		return fmt.Errorf("install update: %w", err)
	}
	if u.PostInstall != nil {
		if err := u.PostInstall(ctx, res.Current, res.Latest); err != nil {
			return fmt.Errorf("post-install step failed (binary was updated): %w", err)
		}
	}
	return nil
}

// SwitchChannel validates channel against the configured specs, shows the
// channel's warning and asks for confirmation unless yes, then persists it.
//
//nolint:revive // CLI output writes always succeed
func (u *Updater) SwitchChannel(channel Channel, yes bool) error {
	specs := u.channels()
	spec, ok := LookupChannel(specs, channel)
	if !ok {
		names := make([]string, 0, len(specs))
		for _, s := range specs {
			names = append(names, string(s.Name))
		}
		return fmt.Errorf("invalid channel %q: must be one of %s", channel, strings.Join(names, ", "))
	}
	if u.Store == nil {
		return ErrNoStore
	}
	if spec.Warning != "" && !yes {
		fmt.Fprintf(u.errOut(), "\n%s\n\n", spec.Warning)
		if !u.confirm(fmt.Sprintf("Switch to %s channel? [y/N] ", channel)) {
			fmt.Fprintln(u.out(), "Cancelled.")
			return nil
		}
	}
	if err := u.Store.SetChannel(channel); err != nil {
		return fmt.Errorf("save update channel: %w", err)
	}
	fmt.Fprintf(u.out(), "Switched to %s channel\n", channel)
	if channel != ChannelStable {
		fmt.Fprintf(u.out(), "Run '%s self update' to get the latest %s build\n", u.Name, channel)
	}
	return nil
}

// confirm prints prompt to Out and reads one line from In. Only "y" or "Y"
// accepts. Read errors (for example a closed stdin) count as a decline.
//
//nolint:revive // CLI output writes always succeed
func (u *Updater) confirm(prompt string) bool {
	fmt.Fprint(u.out(), prompt)
	line, err := bufio.NewReader(u.in()).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	answer := strings.TrimSpace(line)
	return answer == "y" || answer == "Y"
}
