package scripts_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWindowsStaticCgoConfiguration(t *testing.T) {
	for _, tc := range []struct {
		os, tags string
		static   bool
	}{
		{"windows", "", false},
		{"windows", "pgs_static", true},
		{"darwin", "pgs_static", false},
	} {
		t.Run(tc.os+"/"+tc.tags, func(t *testing.T) {
			cmd := exec.Command("go", "list", "-json", "-tags", tc.tags, "../internal/render")
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
				has(pkg.CgoPkgConfig, "--static") != tc.static ||
				has(pkg.CgoLDFLAGS, "-static") != tc.static {
				t.Fatalf("incorrect cgo link mode: %+v", pkg)
			}
		})
	}
}

func scriptPath(t *testing.T, name string) string {
	t.Helper()
	path, err := filepath.Abs(name)
	if err != nil {
		t.Fatal(err)
	}
	return path
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
	cmd := exec.Command(shell, "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-File", scriptPath(t, "test-build-windows.ps1"))
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
	tools := map[string]string{
		"brew": "#!/bin/sh\n[ \"$1\" = --prefix ] || exit 42\nprintf '%s\\n' \"$PGS_TEST_PREFIX\"\n",
		"go": "#!/bin/sh\ncase \"$1\" in\n" +
			"version) printf 'go version " + runtime.Version() + " darwin/" + runtime.GOARCH + "\\n' ;;\n" +
			"env) printf '" + runtime.GOARCH + "\\n' ;;\n" +
			"test) printf 'preflight passed\\n'; exit 43 ;;\n" +
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
