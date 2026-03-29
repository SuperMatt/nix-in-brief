# nix-in-brief

**nib** (Nix In Brief) is a brew-like package manager wrapper for [Nix](https://nixos.org/) on standard Linux distros (Fedora, Ubuntu, etc.).

Nix has one of the largest, most up-to-date package repositories available, including packages that aren't available via Homebrew on Linux (such as [ghostty](https://ghostty.org/)). nib makes it feel familiar.

## Why nib?

- Single command to install, upgrade, search, and remove packages
- No `sudo` required for day-to-day operations
- Packages are always up to date — nixpkgs has ~100,000 packages
- Works on Fedora (including SELinux) and Ubuntu out of the box
- `nib health` and `nib doctor` to keep your system integration in good shape

## Installation

### Quick start (installs Nix too)

```bash
git clone https://github.com/SuperMatt/nix-in-brief.git
cd nix-in-brief
make install
nib setup
```

`nib setup` installs Nix via the [Determinate Systems installer](https://install.determinate.systems/) (handles Fedora SELinux automatically) and places `nib` in `~/.local/bin`.

### If Nix is already installed

```bash
make install
```

Requires Go 1.21+.

## Usage

```
nib install <pkg>...     Install packages from nixpkgs
nib remove <pkg>...      Remove installed packages
nib upgrade              Upgrade all installed packages (skips pinned)
nib outdated             Show packages with newer versions available
nib pin <pkg>...         Pin packages so they are skipped during upgrade
nib unpin <pkg>...       Unpin packages
nib search <term>        Search nixpkgs
nib info <pkg>           Show package details
nib list                 List installed packages
nib rollback             Undo the last install/remove/upgrade

nib health               Show status of nib prerequisites
nib doctor               Diagnose and fix issues interactively

nib setup                Install Nix and copy nib to ~/.local/bin
nib uninstall-nix        Fully remove Nix and all packages

nib version              Print the nib version
```

Pass `-v` / `--verbose` to any command to see unfiltered nix output (warnings, traces, evaluation messages are suppressed by default).

### Examples

```bash
nib install ghostty helix chezmoi starship
nib install nerd-fonts.monaspace
nib search ripgrep
nib upgrade
nib rollback
```

### Fonts

Nix installs fonts to `~/.nix-profile/share/fonts`. On non-NixOS systems fontconfig doesn't know to look there by default. Run `nib doctor` to register the font path automatically.

## Building

```bash
make install   # build and install to ~/.local/bin/nib
make clean     # remove installed binary
```

## Development

### Running tests

```bash
make test
```

### Versioning

Versions come from git tags. `make install` injects the version at build time using `git describe --tags --always --dirty`:

- Clean tagged build → `v0.1.0`
- Commits after a tag → `v0.1.0-3-gabcdef`
- Uncommitted changes → `v0.1.0-dirty`

To cut a release:

```bash
git tag v0.2.0
git push --tags
make install   # nib version → v0.2.0
```

For remote installs (`go install github.com/SuperMatt/nix-in-brief/cmd/nib@latest`), Go's build info provides the version automatically — no ldflags needed.

## Requirements

- Linux (Fedora or Ubuntu recommended)
- Go 1.21+ (to build)
- Nix (installed automatically by `nib setup`)

## License

[MIT](LICENSE)
