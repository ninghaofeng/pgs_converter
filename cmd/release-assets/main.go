// Command release-assets verifies CI packages and prepares versioned release assets.
package main

import (
	"crypto/sha256"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var releaseTag = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)

var targets = []struct{ platform, arch, extension string }{
	{"macos", "amd64", ".tar.gz"},
	{"macos", "arm64", ".tar.gz"},
	{"windows", "amd64", ".zip"},
	{"windows", "arm64", ".zip"},
}

func prepare(tag, input, output string) error {
	if !releaseTag.MatchString(tag) {
		return fmt.Errorf("invalid stable release tag %q: expected vMAJOR.MINOR.PATCH", tag)
	}
	type asset struct{ source, name, digest string }
	var assets []asset
	for _, target := range targets {
		prefix := "pgs-converter-" + target.platform + "-" + target.arch
		matches, err := filepath.Glob(filepath.Join(input, target.platform+"-"+target.arch, prefix+"-static-*"+target.extension))
		if err != nil {
			return err
		}
		if len(matches) != 1 {
			return fmt.Errorf("%s-%s: expected exactly one archive, found %d", target.platform, target.arch, len(matches))
		}
		source := matches[0]
		digest, err := checksum(source)
		if err != nil {
			return err
		}
		notice, err := os.ReadFile(source + ".sha256")
		if err != nil {
			return err
		}
		fields := strings.Fields(string(notice))
		if len(fields) != 2 || fields[1] != filepath.Base(source) || !strings.EqualFold(fields[0], digest) {
			return fmt.Errorf("checksum verification failed for %s", source)
		}
		name := "pgs-converter-" + tag + "-" + target.platform + "-" + target.arch + target.extension
		assets = append(assets, asset{source, name, digest})
	}
	// Validate every platform before creating any release output.
	if err := os.Mkdir(output, 0755); err != nil {
		return err
	}
	var manifest strings.Builder
	for _, asset := range assets {
		if err := copyFile(asset.source, filepath.Join(output, asset.name)); err != nil {
			return err
		}
		fmt.Fprintf(&manifest, "%s  %s\n", asset.digest, asset.name)
	}
	return os.WriteFile(filepath.Join(output, "SHA256SUMS"), []byte(manifest.String()), 0644)
}

func checksum(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash %s: %w", path, err)
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func copyFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	return errors.Join(copyErr, output.Close())
}

func main() {
	tag := flag.String("tag", "", "stable release tag (vMAJOR.MINOR.PATCH)")
	input := flag.String("input", "artifacts", "downloaded CI artifacts directory")
	output := flag.String("output", "release", "new release assets directory")
	flag.Parse()
	if flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "release-assets: unexpected positional arguments")
		os.Exit(1)
	}
	if err := prepare(*tag, *input, *output); err != nil {
		fmt.Fprintln(os.Stderr, "release-assets:", err)
		os.Exit(1)
	}
}
