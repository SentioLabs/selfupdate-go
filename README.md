# selfupdate-go

Self-update with release channels (stable, rc, nightly) for Go CLIs that
publish GitHub releases. Used by [arc](https://github.com/SentioLabs/arc) and
[envctl](https://github.com/SentioLabs/envctl). A Rust port lives at
[selfupdate-rs](https://github.com/SentioLabs/selfupdate-rs).

## What it does

- Resolves the newest release for a channel from the GitHub releases API.
  Stable uses the release GitHub marks latest. `rc` and `nightly` match tag
  patterns (`v1.2.3-rc.1`, `v1.2.3-nightly.20260903`). A newer stable release
  always wins over an older prerelease.
- Compares against the running version, including `dev` builds.
- Downloads the release archive for the running OS and architecture, verifies
  it against `checksums.txt`, extracts the binary, and renames it over the
  running executable. No shell, curl, or install script is involved.
- Runs `PreInstall` after the download is verified and `PostInstall` after the
  binary is replaced, so a CLI can stop and restart a daemon around the swap.
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
    Installer: &selfupdate.ArchiveInstaller{},
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

## ArchiveInstaller

The zero value works for a goreleaser project with default archive names.

| Field | Default | Purpose |
|---|---|---|
| `Name` | base name of the running executable | binary name inside the archive |
| `AssetTemplate` | `{name}_{version}_{os}_{arch}.tar.gz` | archive asset name. `{version}` has no leading `v` |
| `ChecksumAsset` | `checksums.txt` | goreleaser checksum file |
| `SkipChecksum` | `false` | set when the repo publishes no checksum file |
| `TargetPath` | running executable, symlinks resolved | file to replace |
| `Managed` | Homebrew Cellar, `/usr/bin`, `/usr/lib`, Nix store | locations owned by a package manager |
| `Client` | default transport with a 30s response header timeout | HTTP client |
| `Out` | `os.Stdout` | progress lines |

`Prepare` runs before any hook. It resolves the target, refuses a binary that
lives under a package manager (the error names the manager and its upgrade
command), refuses an unwritable directory, then downloads and verifies the
archive into a temp directory. Nothing is downloaded when a check fails.
`Commit` renames the extracted binary over the target and, on macOS, re-signs
it ad hoc. On a terminal the download line shows a progress bar. Elsewhere it
is a single line.

Linux and macOS with `.tar.gz` assets are supported. Windows is not.
Verification proves the download matches what the release page lists. It
does not prove who published it. Signature checking is out of scope.

## Hooks

`PreInstall` runs after the new binary is downloaded and verified, right
before it replaces the running one. `PostInstall` runs after the replace.
A `PostInstall` error is returned with a note that the binary was already
updated.

```go
updater.PreInstall = func(ctx context.Context, current, latest string) error {
    if semver.MajorMinor(latest) != semver.MajorMinor(current) {
        if err := backupDatabase(); err != nil {
            return err
        }
    }
    return stopServer(ctx)
}
updater.PostInstall = func(ctx context.Context, current, latest string) error {
    return startServer(ctx)
}
```

## Store

```go
store := selfupdate.FuncStore{
    Get: func() (selfupdate.Channel, error) { return selfupdate.Channel(cfg.Updates.Channel), nil },
    Set: func(c selfupdate.Channel) error { cfg.Updates.Channel = string(c); return saveConfig(cfg) },
}
```

`selfupdate.MemStore` is available for tests and for CLIs that do not persist
a channel.

## ScriptInstaller

`ScriptInstaller` runs `curl -fsSL <ScriptURL> | bash -s -- --force --tag=<tag>`
and is kept for CLIs that have not moved to `ArchiveInstaller` yet. It needs
`bash` and `curl` on PATH, and the script must accept `--force --tag=<tag>` and
download the archive for that tag.

```go
Installer: &selfupdate.ScriptInstaller{
    ScriptURL: "https://raw.githubusercontent.com/acme/mytool/main/scripts/install.sh",
},
```

## Tagging releases so channels work

- Stable: whatever your release tooling tags (`v1.3.0`).
- Release candidates: tag the *next* version with a dotted counter,
  `v1.4.0-rc.1`, `v1.4.0-rc.2`. For compatibility, the updater compares
  legacy counters numerically too: `rc9` sorts below `rc10`, and `rc10`
  compares equal to `rc.10`. Displayed tags, hook arguments, and asset names
  retain their original spelling.
- Nightlies: tag the *next patch* version, `v1.3.1-nightly.20260904`. A
  nightly tagged with the current released version sorts below that release
  and is never offered.

## Migrating from 0.1

- The import path is `github.com/sentiolabs/selfupdate-go`. The package name
  is still `selfupdate`.
- `Installer` is two-phase. `Install(ctx, tag)` became
  `Prepare(ctx, rel) (Staged, error)`, and `Staged` has `Commit(ctx)` and
  `Close()`. Custom installers implement both. `ScriptInstaller` already does.
- `Resolve` returns a `Release` instead of a tag string. `CheckResult` has a
  `Release` field. `Latest` still holds the tag.
- `Release` carries `Assets`. `Updater` gains `PostInstall`.

## Compatibility

Go 1.26 or later. The root package depends only on the standard library and
`golang.org/x/mod`. The `cobracmd` package adds `github.com/spf13/cobra`.

The API is 0.x and may change until arc and envctl have both shipped on it.
Pin a tag and read the release notes before upgrading.
