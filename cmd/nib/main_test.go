package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── isNixWarning ──────────────────────────────────────────────────────────────

func TestIsNixWarning(t *testing.T) {
	warnings := []string{
		"warning: 'vimPlugins.codeium-nvim' was renamed to 'vimPlugins.windsurf-nvim'",
		"Warning: something deprecated",
		"evaluation warning: attribute 'foo' is deprecated",
		"trace: while evaluating the attribute",
	}
	for _, line := range warnings {
		if !isNixWarning(line) {
			t.Errorf("expected %q to be recognised as a warning", line)
		}
	}

	notWarnings := []string{
		"error: package not found",
		"building '/nix/store/...'",
		"copying path '/nix/store/...'",
		"downloading 'https://cache.nixos.org/...'",
		"",
	}
	for _, line := range notWarnings {
		if isNixWarning(line) {
			t.Errorf("expected %q NOT to be recognised as a warning", line)
		}
	}
}

// ── versionFromStorePath ──────────────────────────────────────────────────────

func TestVersionFromStorePath(t *testing.T) {
	tests := []struct {
		storePath string
		pkgName   string
		want      string
	}{
		{
			storePath: "/nix/store/lifhqxnrjgibp2nv9hk7pmj3xh694189-monaspace-1.301",
			pkgName:   "monaspace",
			want:      "1.301",
		},
		{
			storePath: "/nix/store/abc123xxxxxxxxxxxxxxxxxxxxxxxx-helix-25.0",
			pkgName:   "helix",
			want:      "25.0",
		},
		{
			storePath: "/nix/store/abc123xxxxxxxxxxxxxxxxxxxxxxxx-starship-1.21.1",
			pkgName:   "starship",
			want:      "1.21.1",
		},
		{
			storePath: "/nix/store/abc123xxxxxxxxxxxxxxxxxxxxxxxx-chezmoi-2.50.0",
			pkgName:   "chezmoi",
			want:      "2.50.0",
		},
		{
			storePath: "/nix/store/abc123xxxxxxxxxxxxxxxxxxxxxxxx-monaspace-nerd-fonts-3.4.0",
			pkgName:   "nerd-fonts.monaspace",
			want:      "3.4.0",
		},
		{
			// version with + separator
			storePath: "/nix/store/abc123xxxxxxxxxxxxxxxxxxxxxxxx-nerd-fonts-monaspace-3.4.0+1.200",
			pkgName:   "nerd-fonts.monaspace",
			want:      "3.4.0+1.200",
		},
	}

	for _, tt := range tests {
		t.Run(tt.pkgName, func(t *testing.T) {
			got := versionFromStorePath(tt.storePath, tt.pkgName)
			if got != tt.want {
				t.Errorf("versionFromStorePath(%q, %q) = %q, want %q",
					tt.storePath, tt.pkgName, got, tt.want)
			}
		})
	}
}

// ── pinned packages ───────────────────────────────────────────────────────────

func TestLoadPinnedEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pinned := loadPinned()
	if len(pinned) != 0 {
		t.Errorf("expected empty pinned map, got %v", pinned)
	}
}

func TestSaveAndLoadPinned(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	input := map[string]bool{"helix": true, "ghostty": true, "starship": true}
	if err := savePinned(input); err != nil {
		t.Fatalf("savePinned: %v", err)
	}

	got := loadPinned()
	if len(got) != len(input) {
		t.Errorf("expected %d pinned packages, got %d", len(input), len(got))
	}
	for pkg := range input {
		if !got[pkg] {
			t.Errorf("expected %q to be pinned", pkg)
		}
	}
}

func TestSavePinnedCreatesDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := savePinned(map[string]bool{"helix": true}); err != nil {
		t.Fatalf("savePinned: %v", err)
	}

	if _, err := os.Stat(pinnedFile()); err != nil {
		t.Errorf("pinned file not created: %v", err)
	}
}

func TestSavePinnedEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if err := savePinned(map[string]bool{}); err != nil {
		t.Fatalf("savePinned: %v", err)
	}

	loaded := loadPinned()
	if len(loaded) != 0 {
		t.Errorf("expected empty after saving empty map, got %v", loaded)
	}
}

// ── health checks ─────────────────────────────────────────────────────────────

func TestCheckNixProfileBin(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	bin := filepath.Join(tmpHome, ".nix-profile", "bin")

	t.Run("not in PATH", func(t *testing.T) {
		t.Setenv("PATH", "/usr/bin:/usr/local/bin")
		if checkNixProfileBin().ok {
			t.Error("expected fail when nix profile bin not in PATH")
		}
	})

	t.Run("in PATH", func(t *testing.T) {
		t.Setenv("PATH", "/usr/bin:"+bin+":/usr/local/bin")
		res := checkNixProfileBin()
		if !res.ok {
			t.Errorf("expected ok, got: %s", res.detail)
		}
		if res.detail != bin {
			t.Errorf("expected detail %q, got %q", bin, res.detail)
		}
	})
}

func TestCheckXDGDataDirs(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	share := filepath.Join(tmpHome, ".nix-profile", "share")

	t.Run("not in XDG_DATA_DIRS", func(t *testing.T) {
		t.Setenv("XDG_DATA_DIRS", "/usr/share:/usr/local/share")
		if checkXDGDataDirs().ok {
			t.Error("expected fail when nix share not in XDG_DATA_DIRS")
		}
	})

	t.Run("in XDG_DATA_DIRS", func(t *testing.T) {
		t.Setenv("XDG_DATA_DIRS", "/usr/share:"+share)
		res := checkXDGDataDirs()
		if !res.ok {
			t.Errorf("expected ok, got: %s", res.detail)
		}
	})
}

func TestCheckManPath(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("HOME", tmpHome)
	man := filepath.Join(tmpHome, ".nix-profile", "share", "man")

	t.Run("not in MANPATH", func(t *testing.T) {
		t.Setenv("MANPATH", "/usr/share/man:/usr/local/share/man")
		if checkManPath().ok {
			t.Error("expected fail when nix man not in MANPATH")
		}
	})

	t.Run("in MANPATH", func(t *testing.T) {
		t.Setenv("MANPATH", man+":/usr/share/man")
		res := checkManPath()
		if !res.ok {
			t.Errorf("expected ok, got: %s", res.detail)
		}
	})
}

func TestCheckFontconfig(t *testing.T) {
	t.Run("no config file", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		if checkFontconfig().ok {
			t.Error("expected fail when fontconfig not configured")
		}
	})

	t.Run("config file exists", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		conf := fontconfigConf()
		if err := os.MkdirAll(filepath.Dir(conf), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(conf, []byte("<fontconfig/>"), 0644); err != nil {
			t.Fatal(err)
		}
		res := checkFontconfig()
		if !res.ok {
			t.Errorf("expected ok, got: %s", res.detail)
		}
	})
}

func TestFixFontconfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	if checkFontconfig().ok {
		t.Fatal("precondition: expected fontconfig check to fail before fix")
	}

	if err := fixFontconfig(); err != nil {
		t.Fatalf("fixFontconfig: %v", err)
	}

	if !checkFontconfig().ok {
		t.Error("expected fontconfig check to pass after fix")
	}

	// Verify the written file references the nix fonts dir
	data, err := os.ReadFile(fontconfigConf())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), nixFontsDir()) {
		t.Errorf("fontconfig file does not reference %q:\n%s", nixFontsDir(), data)
	}
}
