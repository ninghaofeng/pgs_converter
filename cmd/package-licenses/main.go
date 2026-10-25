// Command package-licenses collects notices from the actual Go dependency graph,
// including embedded toolkit fonts and bundled native graphics sources.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type module struct {
	Path, Version, Dir string
	Main               bool
}

// collect uses the selected target's dependency graph, not all modules in go.mod.
// Native-library and project-owned asset notices are added by the platform scripts.
func collect(target, output string) error {
	cmd := exec.Command("go", "list", "-deps", "-json", target)
	data, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("list dependencies: %w: %s", err, exit.Stderr)
		}
		return err
	}
	modules := make(map[string]module)
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	for {
		var pkg struct{ Module *module }
		if err := decoder.Decode(&pkg); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		if m := pkg.Module; m != nil && !m.Main {
			modules[m.Path] = *m
		}
	}
	var names []string
	for name := range modules {
		names = append(names, name)
	}
	sort.Strings(names)
	if err := os.MkdirAll(output, 0755); err != nil {
		return err
	}
	var manifest strings.Builder
	for _, name := range names {
		m := modules[name]
		if m.Dir == "" {
			return fmt.Errorf("missing source directory for %s", name)
		}
		count := 0
		// A module may embed fonts or native sources with nested, independent
		// notices. Preserve those relative paths, not just the root LICENSE.
		err := filepath.WalkDir(m.Dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			base := strings.ToUpper(entry.Name())
			if !strings.Contains(base, "LICENSE") && !strings.Contains(base, "LICENCE") &&
				!strings.Contains(base, "COPYING") && !strings.Contains(base, "NOTICE") &&
				base != "PATENTS" && base != "OFL.TXT" && base != "AUTHORS" {
				return nil
			}
			rel, err := filepath.Rel(m.Dir, path)
			if err != nil {
				return err
			}
			destination := filepath.Join(output, name+"@"+m.Version, rel)
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
				return err
			}
			if err := os.WriteFile(destination, data, 0644); err != nil {
				return err
			}
			count++
			return nil
		})
		if err != nil {
			return fmt.Errorf("%s notices: %w", name, err)
		}
		if count == 0 {
			return fmt.Errorf("no license notices found for %s", name)
		}
		fmt.Fprintf(&manifest, "%s %s (%d notice files)\n", name, m.Version, count)
	}
	return os.WriteFile(filepath.Join(output, "MODULES.txt"), []byte(manifest.String()), 0644)
}

func main() {
	target := flag.String("package", "./cmd/pgs-converter-gui", "command whose dependencies are packaged")
	output := flag.String("output", "", "license destination")
	flag.Parse()
	if *output == "" {
		fmt.Fprintln(os.Stderr, "-output is required")
		os.Exit(2)
	}
	if err := collect(*target, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
