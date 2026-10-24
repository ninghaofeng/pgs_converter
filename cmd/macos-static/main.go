// macos-static prepares self-contained pkg-config metadata with explicit native
// archives. Darwin's linker has no supported global third-party-only -static.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

func splitFlags(text string) ([]string, error) {
	var result []string
	var token strings.Builder
	var quote rune
	escaped, started := false, false
	for _, c := range text {
		switch {
		case escaped:
			token.WriteRune(c)
			escaped = false
		case c == '\\' && quote != '\'':
			escaped, started = true, true
		case quote != 0:
			if c == quote {
				quote = 0
			} else {
				token.WriteRune(c)
			}
		case c == '\'' || c == '"':
			quote, started = c, true
		case unicode.IsSpace(c):
			if started {
				result = append(result, token.String())
				token.Reset()
				started = false
			}
		default:
			token.WriteRune(c)
			started = true
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated quoting in pkg-config output")
	}
	if started {
		result = append(result, token.String())
	}
	return result, nil
}

func quoteFlags(flags []string) string {
	quoted := make([]string, len(flags))
	for i, value := range flags {
		quoted[i] = "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}

func staticFlags(flags, extraDirs []string, sdk string) ([]string, []string, error) {
	dirs := append([]string{}, extraDirs...)
	for _, value := range flags {
		if strings.HasPrefix(value, "-L") {
			dirs = append(dirs, strings.TrimPrefix(value, "-L"))
		}
	}
	var output, archives []string
	for _, value := range flags {
		if strings.HasPrefix(value, "-L") {
			continue
		}
		if !strings.HasPrefix(value, "-l") {
			output = append(output, value)
			continue
		}
		name := strings.TrimPrefix(value, "-l")
		if name == "" || strings.ContainsAny(name, `/\:`) {
			return nil, nil, fmt.Errorf("unsupported library flag: %s", value)
		}
		archive := ""
		for _, dir := range dirs {
			path := filepath.Join(dir, "lib"+name+".a")
			info, err := os.Stat(path)
			if err == nil && info.Mode().IsRegular() {
				archive = path
				break
			}
			if err != nil && !os.IsNotExist(err) {
				return nil, nil, err
			}
		}
		if archive != "" {
			resolved, err := filepath.EvalSymlinks(archive)
			if err != nil {
				return nil, nil, err
			}
			output = append(output, resolved)
			archives = append(archives, resolved)
			continue
		}
		// A .tbd in the selected SDK identifies an OS-provided library,
		// including libSystem, libc++, iconv, zlib, bzip2 and HVF.
		if info, err := os.Stat(filepath.Join(sdk, "usr/lib/lib"+name+".tbd")); err == nil && info.Mode().IsRegular() {
			output = append(output, value)
		} else {
			return nil, nil, fmt.Errorf("static archive lib%s.a is missing; searched %s (no dylib fallback)", name, strings.Join(dirs, ", "))
		}
	}
	return output, archives, nil
}

func configure(pkgConfig, sdk, cellar, extraDir, output string) error {
	var err error
	cellar, err = filepath.EvalSymlinks(cellar)
	if err != nil {
		return err
	}
	if extraDir != "" {
		extraDir, err = filepath.EvalSymlinks(extraDir)
		if err != nil {
			return err
		}
	}
	query := func(args ...string) (string, error) {
		cmd := exec.Command(pkgConfig, append([]string{"--static"}, args...)...)
		var stderr strings.Builder
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			return "", fmt.Errorf("pkg-config %v: %w: %s", args, err, stderr.String())
		}
		return strings.TrimSpace(string(data)), nil
	}
	libs, err := query("--libs", "libass")
	if err != nil {
		return err
	}
	flags, err := splitFlags(libs)
	if err != nil {
		return err
	}
	dirs := []string{}
	if extraDir != "" {
		dirs = append(dirs, extraDir)
	}
	resolved, archives, err := staticFlags(flags, dirs, sdk)
	if err != nil {
		return err
	}
	cflags, err := query("--cflags", "libass")
	if err != nil {
		return err
	}
	version, err := query("--modversion", "libass")
	if err != nil {
		return err
	}
	prefixes := map[string]bool{}
	for _, archive := range archives {
		rel, err := filepath.Rel(cellar, archive)
		if err != nil {
			return err
		}
		parts := strings.Split(rel, string(filepath.Separator))
		if len(parts) >= 3 && parts[0] != ".." {
			prefixes[filepath.Join(cellar, parts[0], parts[1])] = true
		} else if extraDir != "" && filepath.Dir(archive) == extraDir {
			prefixes[filepath.Dir(extraDir)] = true
		} else {
			return fmt.Errorf("cannot determine native license/source prefix for %s", archive)
		}
	}
	var sorted []string
	for prefix := range prefixes {
		sorted = append(sorted, prefix)
	}
	sort.Strings(sorted)
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	files := map[string]string{
		"libass.pc": "Name: libass\nDescription: static native archives for pgs-converter\nVersion: " + version +
			"\nCflags: " + cflags + "\nLibs: " + quoteFlags(resolved) + "\n",
		"native-prefixes.txt": strings.Join(sorted, "\n") + "\n",
		"static-archives.txt": strings.Join(archives, "\n") + "\n",
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(output, name), []byte(data), 0644); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	pkgConfig := flag.String("pkg-config", "pkg-config", "Original pkg-config executable")
	sdk := flag.String("sdk", "", "Selected macOS SDK")
	cellar := flag.String("cellar", "", "Homebrew Cellar directory")
	extraDir := flag.String("extra-lib-dir", "", "Private static library directory")
	output := flag.String("output", "", "Generated pkg-config and manifest directory")
	flag.Parse()
	if *sdk == "" || *cellar == "" || *output == "" {
		fmt.Fprintln(os.Stderr, "macos-static: -sdk, -cellar and -output are required")
		os.Exit(2)
	}
	if err := configure(*pkgConfig, *sdk, *cellar, *extraDir, *output); err != nil {
		fmt.Fprintln(os.Stderr, "macos-static:", err)
		os.Exit(1)
	}
}
