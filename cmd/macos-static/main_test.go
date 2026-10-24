package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestFlagQuoting(t *testing.T) {
	want := []string{"-L/path with spaces", "single'quote", `back\slash`, "-framework", "CoreText"}
	got, err := splitFlags(quoteFlags(want))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("flag roundtrip: %v %v", got, err)
	}
	for _, text := range []string{`"unterminated`, `trailing\`} {
		if _, err := splitFlags(text); err == nil {
			t.Fatalf("accepted malformed flags: %s", text)
		}
	}
}

func TestStaticArchiveResolution(t *testing.T) {
	root := t.TempDir()
	native, sdk := filepath.Join(root, "native libs"), filepath.Join(root, "SDK")
	for _, dir := range []string{native, filepath.Join(sdk, "usr/lib")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	for _, path := range []string{filepath.Join(native, "libass.a"), filepath.Join(native, "libgraphite2.dylib"), filepath.Join(sdk, "usr/lib/libSystem.tbd")} {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	flags, archives, err := staticFlags([]string{"-L" + native, "-lass", "-lSystem", "-framework", "CoreText"}, nil, sdk)
	if err != nil || len(archives) != 1 || flags[0] != archives[0] || flags[1] != "-lSystem" {
		t.Fatalf("static/system resolution: %v %v %v", flags, archives, err)
	}
	if _, _, err := staticFlags([]string{"-L" + native, "-lgraphite2"}, nil, sdk); err == nil || !strings.Contains(err.Error(), "no dylib fallback") {
		t.Fatalf("missing archive silently used a dylib: %v", err)
	}
}
