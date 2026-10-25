package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFixture(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func fixtures(t *testing.T) (string, string, string) {
	t.Helper()
	root := t.TempDir()
	input := filepath.Join(root, "artifacts")
	first := ""
	for _, target := range targets {
		dir := filepath.Join(input, target.platform+"-"+target.arch)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		name := "pgs-converter-" + target.platform + "-" + target.arch + "-static-20261025-123456-1" + target.extension
		path := filepath.Join(dir, name)
		data := []byte(target.platform + "/" + target.arch)
		writeFixture(t, path, data)
		writeFixture(t, path+".sha256", []byte(fmt.Sprintf("%x  %s\n", sha256.Sum256(data), name)))
		if first == "" {
			first = path
		}
	}
	return input, filepath.Join(root, "release"), first
}

func TestPrepare(t *testing.T) {
	input, output, _ := fixtures(t)
	if err := prepare("v1.0.0", input, output); err != nil {
		t.Fatal(err)
	}
	var manifest strings.Builder
	for _, target := range targets {
		name := "pgs-converter-v1.0.0-" + target.platform + "-" + target.arch + target.extension
		data, err := os.ReadFile(filepath.Join(output, name))
		want := target.platform + "/" + target.arch
		if err != nil || string(data) != want {
			t.Fatalf("%s: got %q, want %q: %v", name, data, want, err)
		}
		fmt.Fprintf(&manifest, "%x  %s\n", sha256.Sum256(data), name)
	}
	data, err := os.ReadFile(filepath.Join(output, "SHA256SUMS"))
	if err != nil || string(data) != manifest.String() {
		t.Fatalf("incorrect checksum manifest: %s: %v", data, err)
	}
	entries, err := os.ReadDir(output)
	if err != nil || len(entries) != 5 {
		t.Fatalf("expected four archives and SHA256SUMS: %v: %v", entries, err)
	}
}

func TestPrepareRejectsInvalidTags(t *testing.T) {
	for _, tag := range []string{"", "1.0.0", "v1.0", "v01.0.0", "v1.0.0/../../bad", "v1.0.0\n"} {
		t.Run(tag, func(t *testing.T) {
			input, output, _ := fixtures(t)
			if err := prepare(tag, input, output); err == nil {
				t.Fatal("invalid release tag accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("invalid tag created release output: %v", err)
			}
		})
	}
}

func TestPrepareRejectsIncompleteOrCorruptArtifacts(t *testing.T) {
	for _, problem := range []string{"missing-archive", "duplicate-archive", "missing-checksum", "corrupt-archive", "wrong-checksum-name", "extra-checksum-entry"} {
		t.Run(problem, func(t *testing.T) {
			input, output, first := fixtures(t)
			switch problem {
			case "missing-archive":
				if err := os.Remove(first); err != nil {
					t.Fatal(err)
				}
			case "duplicate-archive":
				writeFixture(t, strings.TrimSuffix(first, ".tar.gz")+"-duplicate.tar.gz", []byte("duplicate"))
			case "missing-checksum":
				if err := os.Remove(first + ".sha256"); err != nil {
					t.Fatal(err)
				}
			case "corrupt-archive":
				writeFixture(t, first, []byte("modified after checksum generation"))
			case "wrong-checksum-name":
				digest := sha256.Sum256([]byte("macos/amd64"))
				writeFixture(t, first+".sha256", []byte(fmt.Sprintf("%x  other.tar.gz\n", digest)))
			case "extra-checksum-entry":
				data, err := os.ReadFile(first + ".sha256")
				if err != nil {
					t.Fatal(err)
				}
				writeFixture(t, first+".sha256", append(data, data...))
			}
			if err := prepare("v1.0.0", input, output); err == nil {
				t.Fatal("invalid artifact accepted")
			}
			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("invalid artifact created release output: %v", err)
			}
		})
	}
}

func TestPrepareDoesNotOverwriteOutput(t *testing.T) {
	input, output, _ := fixtures(t)
	if err := os.Mkdir(output, 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(output, "keep")
	writeFixture(t, marker, []byte("existing output"))
	if err := prepare("v1.0.0", input, output); err == nil {
		t.Fatal("existing release directory accepted")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "existing output" {
		t.Fatalf("existing output changed: %v", err)
	}
}
