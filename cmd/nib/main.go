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
	determinateInstall = "https://install.determinate.systems/nix"
	nixInstallerPath   = "/nix/nix-installer"
)

func localBin() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "bin")
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
		// Strip "legacyPackages.<system>." prefix to get the install name
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

func exitOnErr(err error) {
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

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
		onPath := false
		for _, p := range filepath.SplitList(path) {
			if p == bin {
				onPath = true
				break
			}
		}
		if !onPath {
			fmt.Printf("\nAdd %s to your PATH to use 'nib' from anywhere.\n", bin)
		}
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
		return nix(append([]string{"profile", "install"}, pkgs...)...)
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
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" {
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
