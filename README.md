# selfupdate-go

Self-update with release channels (stable, rc, nightly) for Go CLIs that
publish GitHub releases. Used by [arc](https://github.com/SentioLabs/arc) and
[envctl](https://github.com/SentioLabs/envctl).

## What it does

- Resolves the newest release for a channel from the GitHub releases API.
  Stable uses the release GitHub marks latest. `rc` and `nightly` match tag
  patterns (`v1.2.3-rc.1`, `v1.2.3-nightly.20260903`). A newer stable release
  always wins over an older prerelease.
- Compares against the running version, including `dev` builds.
- Prints status and asks for confirmation. Runs an optional pre-install hook,
  then replaces the binary through your existing install script.
- Persists the chosen channel wherever you keep config, through a two-method
  `Store` interface.

## Usage

```go
import (
    "github.com/sentiolabs/selfupdate-go"
    "github.com/sentiolabs/selfupdate-go/cobracmd"
)

updater := &selfupdate.Updater{
    Name:      "mytool",
    Version:   version.Version, // "v1.2.3", "1.2.3" or "dev"
    Source:    &selfupdate.GitHubSource{Owner: "acme", Repo: "mytool"},
    Store:     myChannelStore(), // implements selfupdate.Store
    Installer: &selfupdate.ScriptInstaller{ScriptURL: "https://raw.githubusercontent.com/acme/mytool/main/scripts/install.sh"},
}
rootCmd.AddCommand(cobracmd.New(updater))
```

That adds:

```text
mytool self update [--check] [--force] [-y]
mytool self channel [stable|rc|nightly] [-y]
```

`--check` has no shorthand by default. Pass `cobracmd.WithCheckShorthand("c")`
if your root command does not already use `-c`.

### Store

```go
store := selfupdate.FuncStore{
    Get: func() (selfupdate.Channel, error) { return selfupdate.Channel(cfg.Updates.Channel), nil },
    Set: func(c selfupdate.Channel) error { cfg.Updates.Channel = string(c); return saveConfig(cfg) },
}
```

`selfupdate.MemStore` is available for tests and for CLIs that do not persist
a channel.

### Pre-install hook

```go
updater.PreInstall = func(ctx context.Context, current, latest string) error {
    if semver.MajorMinor(latest) != semver.MajorMinor(current) {
        return backupDatabase()
    }
    return nil
}
```

## Tagging releases so channels work

- Stable: whatever your release tooling tags (`v1.3.0`).
- Release candidates: tag the *next* version with a dotted counter,
  `v1.4.0-rc.1`, `v1.4.0-rc.2`. For compatibility, the updater compares
  legacy counters numerically too: `rc9` sorts below `rc10`, and `rc10`
  compares equal to `rc.10`. Displayed tags, pre-install hook arguments, and
  installer tags retain their original spelling.
- Nightlies: tag the *next patch* version, `v1.3.1-nightly.20260904`. A
  nightly tagged with the current released version sorts below that release
  and is never offered.

The install script must accept `--force --tag=<tag>` and download the
archive for that tag.

## Compatibility

Go 1.26 or later. The root package depends only on the standard library and
`golang.org/x/mod`. The `cobracmd` package adds `github.com/spf13/cobra`.

The API is 0.x and may change until arc and envctl have both shipped on it.
Pin a tag and read the release notes before upgrading.
