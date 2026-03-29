// nib (Nix In Brief) — brew-like wrapper for nix profile
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

const (
	nixProfileSource   = "/nix/var/nix/profiles/default/etc/profile.d/nix-daemon.sh"
	nixBinary          = "/nix/var/nix/profiles/default/bin/nix"
	determinateInstall = "https://install.determinate.systems/nix"
	nixInstallerPath   = "/nix/nix-installer"
)

// version is set at build time via -ldflags "-X main.version=<tag>".
// Falls back to module version (remote go install) then "dev".
var version = "dev"

func init() {
	if version == "dev" {
		if info, ok := debug.ReadBuildInfo(); ok &&
			info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}
}

var (
	green   = "\033[32m"
	red     = "\033[31m"
	yellow  = "\033[33m"
	bold    = "\033[1m"
	reset   = "\033[0m"
	ok      = green + "✓" + reset
	fail    = red + "✘" + reset
	warn    = yellow + "⚠" + reset
	verbose bool
)

// ── paths ─────────────────────────────────────────────────────────────────────

func localBin() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

func nixFontsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nix-profile", "share", "fonts")
}

func nixProfileBin() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nix-profile", "bin")
}

func nixProfileShare() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nix-profile", "share")
}

func nixProfileMan() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nix-profile", "share", "man")
}

func fontconfigConf() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "fontconfig", "conf.d", "10-nix-fonts.conf")
}

func pinnedFile() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "nib", "pinned")
}

// ── nix helpers ───────────────────────────────────────────────────────────────

func nixFound() bool {
	_, err := exec.LookPath("nix")
	return err == nil
}

func ensureNix() {
	if nixFound() {
		return
	}
	if _, err := os.Stat(nixProfileSource); err == nil {
		fmt.Fprintln(os.Stderr, "nix found but not on PATH. Re-run after sourcing your shell profile.")
	} else {
		fmt.Fprintln(os.Stderr, "error: nix not found. Run 'nib setup' first.")
	}
	os.Exit(1)
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

// runCmdFiltered streams stdout normally but strips nix warning lines from stderr.
// When verbose is true, all stderr is passed through unfiltered.
func runCmdFiltered(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin

	if verbose {
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	scanner := bufio.NewScanner(stderrPipe)
	for scanner.Scan() {
		line := scanner.Text()
		if !isNixWarning(line) {
			fmt.Fprintln(os.Stderr, line)
		}
	}

	return cmd.Wait()
}

func isNixWarning(line string) bool {
	lower := strings.ToLower(line)
	return strings.HasPrefix(lower, "warning:") ||
		strings.HasPrefix(lower, "evaluation warning:") ||
		strings.HasPrefix(lower, "trace:")
}

func nix(args ...string) error {
	return runCmdFiltered("nix", args...)
}

// ── pinned packages ───────────────────────────────────────────────────────────

func loadPinned() map[string]bool {
	pinned := map[string]bool{}
	data, err := os.ReadFile(pinnedFile())
	if err != nil {
		return pinned
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			pinned[line] = true
		}
	}
	return pinned
}

func savePinned(pinned map[string]bool) error {
	if err := os.MkdirAll(filepath.Dir(pinnedFile()), 0755); err != nil {
		return err
	}
	var lines []string
	for pkg := range pinned {
		lines = append(lines, pkg)
	}
	sort.Strings(lines)
	return os.WriteFile(pinnedFile(), []byte(strings.Join(lines, "\n")+"\n"), 0644)
}

// ── nix profile list ──────────────────────────────────────────────────────────

type profileElement struct {
	AttrPath   string   `json:"attrPath"`
	StorePaths []string `json:"storePaths"`
}

type profileList struct {
	Elements map[string]profileElement `json:"elements"`
}

func getInstalledPackages() (map[string]profileElement, error) {
	cmd := exec.Command("nix", "profile", "list", "--json")
	if verbose {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = io.Discard
	}
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var profile profileList
	if err := json.Unmarshal(out, &profile); err != nil {
		return nil, err
	}
	return profile.Elements, nil
}

// profileGeneration returns the current nix profile symlink target,
// which changes whenever a profile generation is created.
func profileGeneration() string {
	home, _ := os.UserHomeDir()
	target, err := os.Readlink(filepath.Join(home, ".nix-profile"))
	if err != nil {
		return ""
	}
	return target
}

// versionFromStorePath extracts the version from a nix store path.
// e.g. /nix/store/<hash>-helix-25.0 → "25.0"
func versionFromStorePath(storePath, pkgName string) string {
	base := filepath.Base(storePath)
	// strip the <hash>- prefix
	if idx := strings.Index(base, "-"); idx >= 0 {
		base = base[idx+1:]
	}
	// strip the package name prefix (handles dots-as-hyphens)
	normalised := strings.ReplaceAll(pkgName, ".", "-")
	if after, found := strings.CutPrefix(base, normalised+"-"); found {
		return after
	}
	// fallback: last hyphen-separated segment starting with a digit
	parts := strings.Split(base, "-")
	for i := len(parts) - 1; i >= 0; i-- {
		if len(parts[i]) > 0 && parts[i][0] >= '0' && parts[i][0] <= '9' {
			return strings.Join(parts[i:], "-")
		}
	}
	return base
}

// ── health checks ─────────────────────────────────────────────────────────────

type checkResult struct {
	ok     bool
	detail string
}

type check struct {
	label   string
	run     func() checkResult
	fixDesc string
	fix     func() error
}

func checkNixInstalled() checkResult {
	if _, err := os.Stat(nixBinary); err == nil {
		return checkResult{true, nixBinary}
	}
	if p, err := exec.LookPath("nix"); err == nil {
		return checkResult{true, p}
	}
	return checkResult{false, "not found — run 'nib setup'"}
}

func checkNixOnPath() checkResult {
	if p, err := exec.LookPath("nix"); err == nil {
		return checkResult{true, p}
	}
	if _, err := os.Stat(nixProfileSource); err == nil {
		return checkResult{false, "source " + nixProfileSource + " or restart your shell"}
	}
	return checkResult{false, "nix not installed"}
}

func checkNibOnPath() checkResult {
	if p, err := exec.LookPath("nib"); err == nil {
		return checkResult{true, p}
	}
	return checkResult{false, "add " + localBin() + " to PATH"}
}

func checkNixProfileBin() checkResult {
	bin := nixProfileBin()
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == bin {
			return checkResult{true, bin}
		}
	}
	return checkResult{false, bin + " not in PATH — nix-installed binaries won't be found"}
}

func checkXDGDataDirs() checkResult {
	share := nixProfileShare()
	for _, d := range filepath.SplitList(os.Getenv("XDG_DATA_DIRS")) {
		if d == share {
			return checkResult{true, share}
		}
	}
	return checkResult{false, share + " not in XDG_DATA_DIRS — desktop files and icons may not work"}
}

func checkManPath() checkResult {
	man := nixProfileMan()
	for _, d := range filepath.SplitList(os.Getenv("MANPATH")) {
		if d == man {
			return checkResult{true, man}
		}
	}
	return checkResult{false, man + " not in MANPATH — man pages for nix packages won't be found"}
}

func checkFontconfig() checkResult {
	if _, err := os.Stat(fontconfigConf()); err == nil {
		return checkResult{true, fontconfigConf()}
	}
	return checkResult{false, nixFontsDir() + " not registered with fontconfig"}
}

func checkNixGL() checkResult {
	pkgs, err := getInstalledPackages()
	if err != nil {
		return checkResult{true, "skipped (could not read profile)"}
	}
	gpuApps := []string{"ghostty"}
	for _, app := range gpuApps {
		if _, installed := pkgs[app]; installed {
			if _, err := exec.LookPath("nixGL"); err != nil {
				return checkResult{false, app + " is installed but nixGL not found — GPU acceleration may fail (nib install nixgl.nixGLIntel or nixgl.nixGLNvidia)"}
			}
		}
	}
	return checkResult{true, "ok"}
}

func fixFontconfig() error {
	conf := fontconfigConf()
	if err := os.MkdirAll(filepath.Dir(conf), 0755); err != nil {
		return err
	}
	content := fmt.Sprintf(`<?xml version="1.0"?>
<!DOCTYPE fontconfig SYSTEM "fonts.dtd">
<fontconfig>
  <dir>%s</dir>
</fontconfig>
`, nixFontsDir())
	if err := os.WriteFile(conf, []byte(content), 0644); err != nil {
		return err
	}
	fmt.Println("  Written", conf)
	return nil
}

var healthChecks = []check{
	{label: "nix installed",    run: checkNixInstalled},
	{label: "nix on PATH",      run: checkNixOnPath},
	{label: "nib on PATH",      run: checkNibOnPath},
	{label: "nix profile bin",  run: checkNixProfileBin},
	{label: "XDG_DATA_DIRS",    run: checkXDGDataDirs},
	{label: "MANPATH",          run: checkManPath},
	{
		label:   "fontconfig",
		run:     checkFontconfig,
		fixDesc: "register ~/.nix-profile/share/fonts with fontconfig",
		fix:     fixFontconfig,
	},
	{label: "nixGL",            run: checkNixGL},
}

// ── search ────────────────────────────────────────────────────────────────────

type nixPkg struct {
	Version     string `json:"version"`
	Description string `json:"description"`
}

func searchNixpkgs(terms ...string) error {
	args := append([]string{"search", "--quiet", "--json", "nixpkgs"}, terms...)
	cmd := exec.Command("nix", args...)
	if verbose {
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stderr = io.Discard
	}
	out, err := cmd.Output()
	if err != nil {
		return err
	}

	var packages map[string]nixPkg
	if err := json.Unmarshal(out, &packages); err != nil {
		return err
	}

	if len(packages) == 0 {
		fmt.Println("No packages found.")
		return nil
	}

	keys := make([]string, 0, len(packages))
	for k := range packages {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, key := range keys {
		pkg := packages[key]
		name := key
		if parts := strings.SplitN(key, ".", 3); len(parts) == 3 {
			name = parts[2]
		}
		fmt.Printf("%s (%s)\n", name, pkg.Version)
		if pkg.Description != "" {
			fmt.Printf("  %s\n", pkg.Description)
		}
		fmt.Println()
	}
	return nil
}

// ── commands ──────────────────────────────────────────────────────────────────

var rootCmd = &cobra.Command{
	Use:   "nib",
	Short: "nib (Nix In Brief) — brew-like wrapper for nix profile",
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the nib version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("nib", version)
	},
}

var setupCmd = &cobra.Command{
	Use:   "setup",
	Short: "Install Nix (Determinate Systems) and copy nib to ~/.local/bin",
	RunE: func(cmd *cobra.Command, args []string) error {
		if nixFound() {
			fmt.Println("Nix is already installed.")
		} else {
			fmt.Println("Installing Nix via Determinate Systems...")
			script := fmt.Sprintf(
				"curl --proto '=https' --tlsv1.2 -sSf -L %s | sh -s -- install --no-confirm",
				determinateInstall,
			)
			if err := runCmd("sh", "-c", script); err != nil {
				return err
			}
			fmt.Printf("\nNix installed. You may need to restart your shell or source %s\n", nixProfileSource)
		}

		bin := localBin()
		if err := os.MkdirAll(bin, 0755); err != nil {
			return err
		}
		self, err := os.Executable()
		if err != nil {
			return err
		}
		self, err = filepath.EvalSymlinks(self)
		if err != nil {
			return err
		}
		dest := filepath.Join(bin, "nib")
		if self != dest {
			if err := copyFile(self, dest); err != nil {
				return err
			}
			if err := os.Chmod(dest, 0755); err != nil {
				return err
			}
			fmt.Printf("nib copied to %s\n", dest)
		} else {
			fmt.Printf("nib is already at %s\n", dest)
		}
		for _, p := range filepath.SplitList(os.Getenv("PATH")) {
			if p == bin {
				return nil
			}
		}
		fmt.Printf("\nAdd %s to your PATH to use 'nib' from anywhere.\n", bin)
		return nil
	},
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Show status of nib prerequisites",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("\n%snib health%s\n", bold, reset)
		fmt.Println(strings.Repeat("─", 50))
		allOk := true
		for _, chk := range healthChecks {
			res := chk.run()
			icon := ok
			if !res.ok {
				icon = fail
				allOk = false
			}
			fmt.Printf("  %s  %-20s  %s\n", icon, chk.label, res.detail)
		}
		fmt.Println()
		if allOk {
			fmt.Printf("%s Everything looks good.\n\n", ok)
		} else {
			fmt.Printf("%s Run 'nib doctor' to fix issues.\n\n", warn)
		}
		return nil
	},
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Diagnose and fix issues interactively",
	RunE: func(cmd *cobra.Command, args []string) error {
		type failing struct {
			chk    check
			detail string
		}
		var failures []failing
		for _, chk := range healthChecks {
			res := chk.run()
			if !res.ok {
				failures = append(failures, failing{chk, res.detail})
			}
		}
		if len(failures) == 0 {
			fmt.Printf("%s Nothing to fix — all checks pass.\n", ok)
			return nil
		}
		fmt.Printf("\n%snib doctor%s\n", bold, reset)
		fmt.Println(strings.Repeat("─", 50))
		reader := bufio.NewReader(os.Stdin)
		for _, f := range failures {
			fmt.Printf("\n  %s  %s: %s\n", fail, f.chk.label, f.detail)
			if f.chk.fix == nil {
				fmt.Println("       (no automatic fix available)")
				continue
			}
			if f.chk.fixDesc != "" {
				fmt.Printf("       Fix: %s\n", f.chk.fixDesc)
			}
			fmt.Print("       Apply fix? [y/N] ")
			answer, _ := reader.ReadString('\n')
			if strings.TrimSpace(strings.ToLower(answer)) != "y" {
				continue
			}
			if err := f.chk.fix(); err != nil {
				fmt.Printf("  %s  %s — error: %v\n", warn, f.chk.label, err)
				continue
			}
			res := f.chk.run()
			if res.ok {
				fmt.Printf("  %s  %s — fixed\n", ok, f.chk.label)
			} else {
				fmt.Printf("  %s  %s — still failing: %s\n", warn, f.chk.label, res.detail)
			}
		}
		fmt.Println()
		return nil
	},
}

var installCmd = &cobra.Command{
	Use:   "install <pkg>...",
	Short: "Install packages from nixpkgs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		installed, _ := getInstalledPackages()
		var toInstall []string
		for _, pkg := range args {
			if _, already := installed[pkg]; already {
				fmt.Printf("nib: '%s' is already installed\n", pkg)
			} else {
				toInstall = append(toInstall, pkg)
			}
		}
		if len(toInstall) == 0 {
			return nil
		}
		pkgs := make([]string, len(toInstall))
		for i, p := range toInstall {
			pkgs[i] = "nixpkgs#" + p
		}
		return nix(append([]string{"profile", "add"}, pkgs...)...)
	},
}

var removeCmd = &cobra.Command{
	Use:     "remove <pkg>...",
	Aliases: []string{"uninstall"},
	Short:   "Remove installed packages",
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		installed, err := getInstalledPackages()
		if err != nil {
			return err
		}
		var toRemove []string
		for _, pkg := range args {
			if _, ok := installed[pkg]; ok {
				toRemove = append(toRemove, pkg)
			} else {
				fmt.Printf("nib: '%s' is not installed\n", pkg)
			}
		}
		if len(toRemove) == 0 {
			return nil
		}
		return nix(append([]string{"profile", "remove"}, toRemove...)...)
	},
}

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Aliases: []string{"update"},
	Short:   "Upgrade all installed packages (skips pinned)",
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		pkgs, err := getInstalledPackages()
		if err != nil {
			return err
		}
		if len(pkgs) == 0 {
			fmt.Println("nib: no packages installed")
			return nil
		}
		pinned := loadPinned()
		before := profileGeneration()

		var upgradeErr error
		if len(pinned) == 0 {
			upgradeErr = nix("profile", "upgrade", ".*")
		} else {
			var toUpgrade []string
			for name := range pkgs {
				if !pinned[name] {
					toUpgrade = append(toUpgrade, name)
				}
			}
			if len(toUpgrade) == 0 {
				fmt.Println("nib: all packages are pinned, nothing to upgrade")
				return nil
			}
			sort.Strings(toUpgrade)
			fmt.Printf("nib: upgrading %s (skipping %d pinned)\n", strings.Join(toUpgrade, ", "), len(pinned))
			upgradeErr = nix(append([]string{"profile", "upgrade"}, toUpgrade...)...)
		}

		if upgradeErr == nil && profileGeneration() == before {
			fmt.Println("nib: all packages are up to date")
		}
		return upgradeErr
	},
}

var outdatedCmd = &cobra.Command{
	Use:   "outdated",
	Short: "Show installed packages with newer versions available",
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		pkgs, err := getInstalledPackages()
		if err != nil {
			return err
		}
		if len(pkgs) == 0 {
			fmt.Println("No packages installed.")
			return nil
		}

		type result struct {
			name       string
			current    string
			latest     string
			outdated   bool
		}

		fmt.Printf("Checking %d package(s)...\n", len(pkgs))
		var results []result
		for name, elem := range pkgs {
			current := ""
			if len(elem.StorePaths) > 0 {
				current = versionFromStorePath(elem.StorePaths[0], name)
			}

			searchCmd := exec.Command("nix", "search", "--quiet", "--json", "nixpkgs",
				"^"+strings.ReplaceAll(name, ".", "\\.")+"$")
			if verbose {
				searchCmd.Stderr = os.Stderr
			} else {
				searchCmd.Stderr = io.Discard
			}
			out, err := searchCmd.Output()
			latest := current
			if err == nil {
				var found map[string]nixPkg
				if json.Unmarshal(out, &found) == nil {
					for _, pkg := range found {
						latest = pkg.Version
						break
					}
				}
			}
			results = append(results, result{
				name:     name,
				current:  current,
				latest:   latest,
				outdated: latest != "" && current != "" && latest != current,
			})
		}

		sort.Slice(results, func(i, j int) bool { return results[i].name < results[j].name })

		anyOutdated := false
		for _, r := range results {
			if r.outdated {
				fmt.Printf("  %s  %-30s  %s → %s\n", warn, r.name, r.current, r.latest)
				anyOutdated = true
			}
		}
		if !anyOutdated {
			fmt.Printf("%s All packages are up to date.\n", ok)
		} else {
			fmt.Printf("\nRun 'nib upgrade' to update.\n")
		}
		return nil
	},
}

var pinCmd = &cobra.Command{
	Use:   "pin <pkg>...",
	Short: "Pin packages so they are skipped during upgrade",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pinned := loadPinned()
		for _, pkg := range args {
			pinned[pkg] = true
			fmt.Printf("  %s  pinned %s\n", ok, pkg)
		}
		return savePinned(pinned)
	},
}

var unpinCmd = &cobra.Command{
	Use:   "unpin <pkg>...",
	Short: "Unpin packages so they are included in upgrades",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		pinned := loadPinned()
		for _, pkg := range args {
			if pinned[pkg] {
				delete(pinned, pkg)
				fmt.Printf("  %s  unpinned %s\n", ok, pkg)
			} else {
				fmt.Printf("  %s  %s was not pinned\n", warn, pkg)
			}
		}
		return savePinned(pinned)
	},
}

var searchCmd = &cobra.Command{
	Use:   "search <term>...",
	Short: "Search nixpkgs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		return searchNixpkgs(args...)
	},
}

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List installed packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		return nix("profile", "list")
	},
}

var infoCmd = &cobra.Command{
	Use:   "info <pkg>",
	Short: "Show package details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		return searchNixpkgs("^" + args[0] + "$")
	},
}

var rollbackCmd = &cobra.Command{
	Use:   "rollback",
	Short: "Undo the last install/remove/upgrade",
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		return nix("profile", "rollback")
	},
}

var uninstallNixCmd = &cobra.Command{
	Use:   "uninstall-nix",
	Short: "Fully remove Nix and all packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("This will completely remove Nix and all installed packages.")
		fmt.Print("Are you sure? [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(answer)) != "y" {
			fmt.Println("Aborted.")
			return nil
		}
		installer, err := exec.LookPath("nix-installer")
		if err != nil {
			if _, statErr := os.Stat(nixInstallerPath); statErr == nil {
				installer = nixInstallerPath
			} else {
				return fmt.Errorf("could not find nix-installer. See https://install.determinate.systems/nix")
			}
		}
		if err := runCmd(installer, "uninstall", "--no-confirm"); err != nil {
			return err
		}
		nibBin := filepath.Join(localBin(), "nib")
		if _, err := os.Stat(nibBin); err == nil {
			os.Remove(nibBin)
			fmt.Printf("Removed %s\n", nibBin)
		}
		fmt.Println("Nix uninstalled.")
		return nil
	},
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func main() {
	rootCmd.Version = version
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "show nix warnings and verbose output")
	rootCmd.AddCommand(
		versionCmd,
		setupCmd,
		healthCmd,
		doctorCmd,
		installCmd,
		removeCmd,
		upgradeCmd,
		outdatedCmd,
		pinCmd,
		unpinCmd,
		searchCmd,
		listCmd,
		infoCmd,
		rollbackCmd,
		uninstallNixCmd,
	)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
