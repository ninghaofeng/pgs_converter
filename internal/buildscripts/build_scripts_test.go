package buildscripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestStaticCgoConfiguration(t *testing.T) {
	for _, tc := range []struct {
		os     string
		static bool
	}{
		{"windows", true},
		{"darwin", false},
	} {
		t.Run(tc.os, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-json", "../render")
			cmd.Env = append(os.Environ(), "GOOS="+tc.os, "GOARCH=amd64", "CGO_ENABLED=1")
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("inspect cgo configuration: %v: %s", err, output)
			}
			var pkg struct {
				CgoPkgConfig []string
				CgoLDFLAGS   []string
			}
			if err := json.Unmarshal(output, &pkg); err != nil {
				t.Fatal(err)
			}
			has := func(values []string, want string) bool {
				for _, value := range values {
					if value == want {
						return true
					}
				}
				return false
			}
			if !has(pkg.CgoPkgConfig, "libass") ||
				!has(pkg.CgoPkgConfig, "--static") ||
				has(pkg.CgoLDFLAGS, "-static") != tc.static {
				t.Fatalf("incorrect cgo link mode: %+v", pkg)
			}
		})
	}
}

func TestDesktopTargetPackages(t *testing.T) {
	for _, target := range []struct{ os, arch string }{
		{"windows", "amd64"}, {"windows", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"},
	} {
		t.Run(target.os+"/"+target.arch, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-deps", "../../cmd/pgs-converter-gui")
			cmd.Env = append(os.Environ(), "GOOS="+target.os, "GOARCH="+target.arch, "CGO_ENABLED=1")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("GUI target has unsupported package constraints: %v: %s", err, output)
			}
		})
	}
}

func TestWindowsNativePickerCompilation(t *testing.T) {
	for _, arch := range []string{"amd64", "arm64"} {
		t.Run(arch, func(t *testing.T) {
			cmd := exec.Command("go", "test", "-c", "-o", filepath.Join(t.TempDir(), "nativepicker.test.exe"), "../nativepicker")
			cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH="+arch, "CGO_ENABLED=0")
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("native Windows picker cannot compile: %v: %s", err, output)
			}
		})
	}
}

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(filepath.Join("..", "..", "scripts", name))
	if err != nil {
		t.Fatal(err)
	}
	return path
}

func TestBuildScriptLayout(t *testing.T) {
	entries, err := os.ReadDir(scriptPath(t, "."))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"build-macos.sh", "build-windows.cmd", "build-windows.ps1"}
	if len(entries) != len(want) {
		t.Fatalf("scripts must contain only the three build entry points, got %v", entries)
	}
	for i, entry := range entries {
		if entry.IsDir() || entry.Name() != want[i] {
			t.Fatalf("unexpected scripts entry: %s, want %s", entry.Name(), want[i])
		}
		// Child-process file reads are not tracked by Go's test cache.
		if _, err := os.ReadFile(scriptPath(t, entry.Name())); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMacRejectsDynamicPackages(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS Mach-O audit")
	}
	for _, dependency := range []string{"@rpath/libass.dylib", "/opt/homebrew/lib/libass.dylib", "/usr/local/lib/libass.dylib", ""} {
		t.Run(dependency, func(t *testing.T) {
			root := t.TempDir()
			tool := filepath.Join(root, "otool")
			text := "#!/bin/sh\nprintf 'binary:\\n'\n"
			if dependency != "" {
				text += "printf '\\t" + dependency + " (compatibility version 1.0.0, current version 1.0.0)\\n'\n"
			}
			if err := os.WriteFile(tool, []byte(text), 0755); err != nil {
				t.Fatal(err)
			}
			destination := filepath.Join(root, "package")
			cmd := exec.Command("/bin/bash", "-c", `source "$1"; bundle_macos "$2" "$3" "$4"`,
				"bundle-test", scriptPath(t, "build-macos.sh"), filepath.Join(root, "binary"), destination, root)
			cmd.Env = append(os.Environ(), "PATH="+root+string(os.PathListSeparator)+os.Getenv("PATH"))
			output, err := cmd.CombinedOutput()
			if err == nil || (!strings.Contains(string(output), "non-system") && !strings.Contains(string(output), "cannot verify")) {
				t.Fatalf("accepted unverified/dynamic executable: %v: %s", err, output)
			}
			if _, err := os.Stat(destination); !os.IsNotExist(err) {
				t.Fatalf("failed audit created a success-shaped package: %v", err)
			}
		})
	}
}

func TestMacBuildArguments(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS build entry point")
	}
	script := scriptPath(t, "build-macos.sh")
	for _, tc := range []struct {
		arg  string
		code int
		want string
	}{{"--help", 0, "Usage:"}, {"--unknown-option", 2, "Unknown option:"}} {
		cmd := exec.Command("/bin/bash", script, tc.arg)
		// The entry point must not depend on the caller's current directory.
		cmd.Dir = t.TempDir()
		output, err := cmd.CombinedOutput()
		if cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != tc.code || !strings.Contains(string(output), tc.want) {
			t.Fatalf("%s: %v: %s", tc.arg, err, output)
		}
	}
}

func TestMacDependencyInstallationIsOptIn(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS dependency preflight")
	}
	script := scriptPath(t, "build-macos.sh")
	for _, missing := range []string{"go", "libass"} {
		for _, install := range []bool{false, true} {
			t.Run(missing+map[bool]string{false: "/detect", true: "/install"}[install], func(t *testing.T) {
				root := t.TempDir()
				bin := filepath.Join(root, "bin")
				if err := os.MkdirAll(bin, 0755); err != nil {
					t.Fatal(err)
				}
				// All installation commands are intercepted; this test must
				// never modify Homebrew or the developer's native dependencies.
				tools := map[string]string{
					"brew": `#!/bin/sh
case "$1" in
  --prefix) printf '%s\n' "$PGS_TEST_PREFIX" ;;
  list) exit 1 ;;
  install|upgrade) printf 'install attempted\n' > "$PGS_TEST_MARKER"; exit 42 ;;
  *) exit 1 ;;
esac
`,
					"pkg-config": "#!/bin/sh\nexit 1\n",
				}
				if missing == "go" {
					tools["go"] = "#!/bin/sh\nprintf 'go version go1.0.0 darwin/arm64\\n'\n"
				}
				for name, text := range tools {
					if err := os.WriteFile(filepath.Join(bin, name), []byte(text), 0755); err != nil {
						t.Fatal(err)
					}
				}
				marker := filepath.Join(root, "install-called")
				args := []string{script}
				if install {
					args = append(args, "--install-dependencies")
				}
				cmd := exec.Command("/bin/bash", args...)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"), "PGS_TEST_PREFIX="+root, "PGS_TEST_MARKER="+marker)
				output, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatalf("missing dependency unexpectedly succeeded: %s", output)
				}
				_, statErr := os.Stat(marker)
				if install {
					if statErr != nil || cmd.ProcessState.ExitCode() != 42 {
						t.Fatalf("explicit installer failure was not propagated: %v: %s", statErr, output)
					}
				} else if !os.IsNotExist(statErr) || !strings.Contains(string(output), "--install-dependencies") {
					t.Fatalf("default detection installed dependencies or omitted guidance: %v: %s", statErr, output)
				}
			})
		}
	}
}

func TestPowerShellBuildHelpers(t *testing.T) {
	var shell string
	for _, candidate := range []string{"pwsh", "powershell.exe"} {
		if path, err := exec.LookPath(candidate); err == nil {
			shell = path
			break
		}
	}
	if shell == "" {
		t.Skip("PowerShell unavailable; Windows CI runs syntax and helper tests")
	}
	testScript, err := filepath.Abs(filepath.Join("testdata", "windows.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.ReadFile(testScript); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", testScript)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("PowerShell syntax/helper checks: %v: %s", err, output)
	}
}

func TestMacKeepsDefaultPkgConfigPaths(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS dependency preflight")
	}
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "lib"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib/libgraphite2.a"), nil, 0644); err != nil {
		t.Fatal(err)
	}
	tools := map[string]string{
		"brew": "#!/bin/sh\ncase \"$1\" in --prefix|--cellar) printf '%s\\n' \"$PGS_TEST_PREFIX\";; *) exit 42;; esac\n",
		"go": "#!/bin/sh\ncase \"$1\" in\n" +
			"version) printf 'go version " + runtime.Version() + " darwin/" + runtime.GOARCH + "\\n' ;;\n" +
			"env) printf '" + runtime.GOARCH + "\\n' ;;\n" +
			"run) printf 'preflight passed\\n'; exit 43 ;;\n" +
			"*) exit 44 ;;\nesac\n",
		"pkg-config": `#!/bin/sh
[ -z "${PKG_CONFIG_LIBDIR+x}" ] && [ -z "${PKG_CONFIG_SYSROOT_DIR+x}" ] || exit 1
case "$1" in
  --exists) exit 0 ;;
  --modversion) printf '0.17.5\n' ;;
  *) exit 1 ;;
esac
`,
	}
	for name, text := range tools {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(text), 0755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("/bin/bash", scriptPath(t, "build-macos.sh"))
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"PGS_TEST_PREFIX="+root, "PKG_CONFIG_LIBDIR=/foreign", "PKG_CONFIG_SYSROOT_DIR=/foreign")
	output, err := cmd.CombinedOutput()
	if err == nil || cmd.ProcessState == nil || cmd.ProcessState.ExitCode() != 43 || !strings.Contains(string(output), "preflight passed") {
		t.Fatalf("default pkg-config search paths were not retained: %v: %s", err, output)
	}
}
