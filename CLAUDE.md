# CLAUDE.md

## Project overview

nib (Nix In Brief) is a brew-like wrapper for `nix profile` on Linux. The CLI command is `nib`. Single Go binary, no runtime dependencies beyond Nix itself.

## Structure

```
nix-in-brief/
├── cmd/nib/main.go   — all source code (single file)
├── go.mod            — module: github.com/SuperMatt/nix-in-brief
├── go.sum
├── Makefile          — make install → ~/.local/bin/nib
├── README.md
└── CLAUDE.md
```

## Build

```bash
make install   # builds and installs to ~/.local/bin/nib
```

Requires Go 1.21+. The module is `github.com/SuperMatt/nix-in-brief`. Uses [Cobra](https://github.com/spf13/cobra) for CLI.

## Versioning

`var version = "dev"` is the in-source default. The Makefile injects the real version at build time:

```makefile
go install -ldflags "-X main.version=$(VERSION)" ./cmd/nib/
```

`VERSION` = `git describe --tags --always --dirty`. To release: `git tag vX.Y.Z && git push --tags && make install`.

For remote installs (`go install ...@latest`), `runtime/debug.ReadBuildInfo()` supplies the version as a fallback so ldflags are not required.

See the README Development section for the full workflow.

## Key conventions

- All source lives in `cmd/nib/main.go` — keep it that way unless it grows substantially
- Each subcommand is a `var xxxCmd = &cobra.Command{...}` at package level, registered in `main()`
- Health checks are structs in the `healthChecks` slice — add new checks there, not inline
- Use `runCmd()` for commands that stream output to the terminal; use `exec.Command().Output()` when capturing output for processing
- ANSI colour constants (`ok`, `fail`, `warn`) are defined at the top — use them consistently
- Nix binary paths: `nixBinary` and `nixProfileSource` constants cover the standard Determinate Systems install location

## Warning suppression

nix emits many warnings (`warning:`, `evaluation warning:`, `trace:`) that are noise for end users. `runCmdFiltered()` strips these from stderr by default. The global `verbose bool` flag (registered as `-v`/`--verbose` on `rootCmd`) bypasses filtering when true — set `cmd.Stderr = os.Stderr` (or use `runCmdFiltered` which checks `verbose` automatically) rather than `io.Discard` for any new commands that should respect the flag.

## Pinned packages

Stored in `~/.config/nib/pinned` — one package name per line. `nib upgrade` reads this and skips pinned packages by passing explicit names to `nix profile upgrade` instead of `.*`. Nix has no built-in per-package pin mechanism for imperative profiles, so this is the correct approach.

## nib outdated

Calls `nix profile list --json` to get installed packages and their store paths, extracts current versions from store paths via `versionFromStorePath()`, then calls `nix search --json` per package to get latest versions and diffs them. Can be slow with many packages installed.

## Adding a health check

Add a `check` struct to the `healthChecks` slice in `main.go`:

```go
{
    label:   "my check",
    run:     func() checkResult { ... },   // return checkResult{ok, detail}
    fixDesc: "human-readable description of the fix",
    fix:     func() error { ... },         // nil if no automatic fix
},
```

## Nix notes

- Install: Determinate Systems installer (`https://install.determinate.systems/nix`)
- Uninstall: `nix-installer uninstall --no-confirm`
- Profile location: `~/.nix-profile/`
- Binaries: `~/.nix-profile/bin/`
- Fonts/share: `~/.nix-profile/share/`
- Daemon profile script: `/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh`
- `nix profile add` is the current command (not `install`, which is deprecated)
- `nix search --json --quiet nixpkgs <term>` for machine-readable search results
