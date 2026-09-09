// Package cobracmd adapts a selfupdate.Updater to cobra commands: a "self"
// parent with "update" and "channel" subcommands.
package cobracmd

import (
	"context"
	"fmt"

	"github.com/sentiolabs/selfupdate-go"
	"github.com/spf13/cobra"
)

type options struct {
	checkShorthand string
}

// Option customizes the generated commands.
type Option func(*options)

// WithCheckShorthand sets a one-letter shorthand for --check on the update
// command. Off by default: a root command that already defines the same
// shorthand as a persistent flag (envctl uses -c for --config) would panic.
func WithCheckShorthand(s string) Option {
	return func(o *options) { o.checkShorthand = s }
}

// New returns a "self" command with "update" and "channel" subcommands.
func New(u *selfupdate.Updater, opts ...Option) *cobra.Command {
	self := &cobra.Command{
		Use:   "self",
		Short: fmt.Sprintf("Manage the %s CLI itself", u.Name),
	}
	self.AddCommand(NewUpdate(u, opts...))
	self.AddCommand(NewChannel(u))
	return self
}

// NewUpdate returns the "update" command.
func NewUpdate(u *selfupdate.Updater, opts ...Option) *cobra.Command {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	var flags selfupdate.UpdateOptions
	cmd := &cobra.Command{
		Use:          "update",
		Short:        fmt.Sprintf("Update %s to the latest version", u.Name),
		SilenceUsage: true,
		Long: fmt.Sprintf(`Update %[1]s to the latest release on the current update channel.

Use '%[1]s self channel' to view or switch the channel.

Examples:
  %[1]s self update          Update if a new version is available
  %[1]s self update --check  Check for updates without installing
  %[1]s self update --force  Reinstall even if already up to date
  %[1]s self update -y       Update without a confirmation prompt`, u.Name),
		RunE: func(cmd *cobra.Command, _ []string) error {
			return bound(u, cmd).Update(cmdContext(cmd), flags)
		},
	}
	cmd.Flags().BoolVarP(&flags.Force, "force", "f", false, "reinstall even if already up to date")
	cmd.Flags().BoolVarP(&flags.Yes, "yes", "y", false, "skip the confirmation prompt")
	if o.checkShorthand != "" {
		cmd.Flags().BoolVarP(&flags.Check, "check", o.checkShorthand, false, "check for updates without installing")
	} else {
		cmd.Flags().BoolVar(&flags.Check, "check", false, "check for updates without installing")
	}
	return cmd
}

// NewChannel returns the "channel [name]" command.
func NewChannel(u *selfupdate.Updater) *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use:          "channel [stable|rc|nightly]",
		Short:        "View or switch the update channel",
		SilenceUsage: true,
		Long: fmt.Sprintf(`View or switch the update channel used by '%[1]s self update'.

Channels:
  stable   Official releases (default)
  rc       Release candidates
  nightly  Daily builds from the main branch

Examples:
  %[1]s self channel            Show the current channel
  %[1]s self channel rc         Switch to rc (asks for confirmation)
  %[1]s self channel rc -y      Switch without prompting`, u.Name),
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b := bound(u, cmd)
			if len(args) == 0 {
				ch, err := b.CurrentChannel()
				if err != nil {
					return err
				}
				_, err = fmt.Fprintf(b.Out, "Current update channel: %s\n", ch)
				return err
			}
			return b.SwitchChannel(selfupdate.Channel(args[0]), yes)
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

// bound returns a copy of u whose unset streams point at the command's.
func bound(u *selfupdate.Updater, cmd *cobra.Command) *selfupdate.Updater {
	b := *u
	if b.Out == nil {
		b.Out = cmd.OutOrStdout()
	}
	if b.ErrOut == nil {
		b.ErrOut = cmd.ErrOrStderr()
	}
	if b.In == nil {
		b.In = cmd.InOrStdin()
	}
	return &b
}

func cmdContext(cmd *cobra.Command) context.Context {
	if ctx := cmd.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
