# CLAUDE.md

## Project overview

nibble is a brew-like wrapper for `nix profile` on Linux. The CLI command is `nib`. Single Go binary, no runtime dependencies beyond Nix itself.

## Structure

```
nibble/
├── cmd/nib/main.go   — all source code (single file)
├── go.mod            — module: github.com/SuperMatt/nibble
├── go.sum
├── Makefile          — make install → ~/.local/bin/nib
├── README.md
└── CLAUDE.md
```

## Build

```bash
make install   # builds and installs to ~/.local/bin/nib
```

Requires Go 1.21+. The module is `github.com/SuperMatt/nibble`. Uses [Cobra](https://github.com/spf13/cobra) for CLI.

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

Stored in `~/.config/nibble/pinned` — one package name per line. `nib upgrade` reads this and skips pinned packages by passing explicit names to `nix profile upgrade` instead of `.*`. Nix has no built-in per-package pin mechanism for imperative profiles, so this is the correct approach.

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
