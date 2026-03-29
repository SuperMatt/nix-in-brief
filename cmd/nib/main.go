// nibble (nib) — brew-like wrapper for nix profile
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
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

var (
	green  = "\033[32m"
	red    = "\033[31m"
	yellow = "\033[33m"
	bold   = "\033[1m"
	reset  = "\033[0m"
	ok     = green + "✓" + reset
	fail   = red + "✘" + reset
	warn   = yellow + "⚠" + reset
)

func localBin() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
}

func nixFontsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".nix-profile", "share", "fonts")
}

func fontconfigConf() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "fontconfig", "conf.d", "10-nix-fonts.conf")
}

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

func nix(args ...string) error {
	return runCmd("nix", args...)
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

func checkFontconfig() checkResult {
	conf := fontconfigConf()
	if _, err := os.Stat(conf); err == nil {
		return checkResult{true, conf}
	}
	return checkResult{false, nixFontsDir() + " not registered with fontconfig"}
}

func checkFcCache() checkResult {
	out, err := exec.Command("fc-list", "--format=%{file}\n").Output()
	if err == nil && strings.Contains(string(out), "/nix/") {
		return checkResult{true, "nix fonts visible to fontconfig"}
	}
	if _, err := os.Stat(fontconfigConf()); err != nil {
		return checkResult{false, "fontconfig not configured (fix fontconfig first)"}
	}
	return checkResult{false, "run fc-cache to rebuild font cache"}
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

func fixFcCache() error {
	fmt.Println("  Running fc-cache...")
	return runCmd("fc-cache", "-f", nixFontsDir())
}

var healthChecks = []check{
	{
		label: "nix installed",
		run:   checkNixInstalled,
	},
	{
		label: "nix on PATH",
		run:   checkNixOnPath,
	},
	{
		label: "nib on PATH",
		run:   checkNibOnPath,
	},
	{
		label:   "fontconfig",
		run:     checkFontconfig,
		fixDesc: "register ~/.nix-profile/share/fonts with fontconfig",
		fix:     fixFontconfig,
	},
	{
		label:   "font cache",
		run:     checkFcCache,
		fixDesc: "rebuild fc-cache",
		fix:     fixFcCache,
	},
}

var healthCmd = &cobra.Command{
	Use:   "health",
	Short: "Show status of nib prerequisites",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("\n%snibble health%s\n", bold, reset)
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

		fmt.Printf("\n%snibble doctor%s\n", bold, reset)
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

// ── search ────────────────────────────────────────────────────────────────────

type nixPkg struct {
	Version     string `json:"version"`
	Description string `json:"description"`
}

func searchNixpkgs(terms ...string) error {
	args := append([]string{"search", "--quiet", "--json", "nixpkgs"}, terms...)
	cmd := exec.Command("nix", args...)
	cmd.Stderr = io.Discard
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
	Short: "nibble (nib) — brew-like wrapper for nix profile",
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

		path := os.Getenv("PATH")
		for _, p := range filepath.SplitList(path) {
			if p == bin {
				return nil
			}
		}
		fmt.Printf("\nAdd %s to your PATH to use 'nib' from anywhere.\n", bin)
		return nil
	},
}

var installCmd = &cobra.Command{
	Use:   "install <pkg>...",
	Short: "Install packages from nixpkgs",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		pkgs := make([]string, len(args))
		for i, p := range args {
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
		return nix(append([]string{"profile", "remove"}, args...)...)
	},
}

var upgradeCmd = &cobra.Command{
	Use:     "upgrade",
	Aliases: []string{"update"},
	Short:   "Upgrade all installed packages",
	RunE: func(cmd *cobra.Command, args []string) error {
		ensureNix()
		return nix("profile", "upgrade", ".*")
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
	rootCmd.AddCommand(
		setupCmd,
		healthCmd,
		doctorCmd,
		installCmd,
		removeCmd,
		upgradeCmd,
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
