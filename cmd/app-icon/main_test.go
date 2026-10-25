package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestGeneratedIcons(t *testing.T) {
	assets := filepath.Join("..", "..", "internal", "desktop", "assets")
	source, err := os.ReadFile(filepath.Join(assets, "icon.svg"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := icons(source)
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range generated {
		data, err := os.ReadFile(filepath.Join(assets, name))
		if err != nil || !bytes.Equal(data, want) {
			t.Fatalf("%s is stale: run go run ./cmd/app-icon (%v)", name, err)
		}
	}
	im, err := png.Decode(bytes.NewReader(generated["icon.png"]))
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() != 512 || im.Bounds().Dy() != 512 {
		t.Fatal("wrong application icon dimensions")
	}
	white := 0
	for y := 0; y < 512; y++ {
		for x := 0; x < 512; x++ {
			r, g, b, a := im.At(x, y).RGBA()
			if r == 65535 && g == 65535 && b == 65535 && a == 65535 {
				white++
			}
		}
	}
	if white < 20000 {
		t.Fatalf("subtitle glyph is missing or too small: %d white pixels", white)
	}
	if _, _, _, alpha := im.At(0, 0).RGBA(); alpha != 0 {
		t.Fatal("icon corners should be transparent")
	}
	ico := generated["icon.ico"]
	if binary.LittleEndian.Uint16(ico[4:]) != 6 {
		t.Fatal("Windows icon must contain six resolutions")
	}
	for i := 0; i < 6; i++ {
		entry := ico[6+16*i:]
		start, length := int(binary.LittleEndian.Uint32(entry[12:])), int(binary.LittleEndian.Uint32(entry[8:]))
		size := int(entry[0])
		if size == 0 {
			size = 256
		}
		config, err := png.DecodeConfig(bytes.NewReader(ico[start : start+length]))
		if err != nil || config.Width != size || config.Height != size {
			t.Fatalf("invalid ICO payload %d: %v", size, err)
		}
	}
	icns := generated["icon.icns"]
	if string(icns[:4]) != "icns" || int(binary.BigEndian.Uint32(icns[4:])) != len(icns) {
		t.Fatal("invalid macOS icon header")
	}
	count := 0
	for offset := 8; offset < len(icns); {
		length := int(binary.BigEndian.Uint32(icns[offset+4:]))
		if _, err := png.DecodeConfig(bytes.NewReader(icns[offset+8 : offset+length])); err != nil {
			t.Fatal("invalid ICNS image:", err)
		}
		offset += length
		count++
	}
	if count != 11 {
		t.Fatal("missing macOS standard/Retina icon sizes")
	}
}

func TestWindowsResourcesLinkForBothArchitectures(t *testing.T) {
	for _, target := range []struct {
		arch    string
		machine uint16
	}{{"amd64", pe.IMAGE_FILE_MACHINE_AMD64}, {"arm64", pe.IMAGE_FILE_MACHINE_ARM64}} {
		t.Run(target.arch, func(t *testing.T) {
			dir := t.TempDir()
			resource, err := os.ReadFile(filepath.Join("..", "pgs-converter-gui", "icon_windows_"+target.arch+".syso"))
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "icon_windows_"+target.arch+".syso"), resource, 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
				t.Fatal(err)
			}
			output := filepath.Join(dir, "icon.exe")
			cmd := exec.Command("go", "build", "-o", output, ".")
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "GO111MODULE=off", "GOOS=windows", "GOARCH="+target.arch, "CGO_ENABLED=0")
			if data, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("resource did not link: %v: %s", err, data)
			}
			executable, err := pe.Open(output)
			if err != nil {
				t.Fatal(err)
			}
			defer executable.Close()
			if executable.Machine != target.machine {
				t.Fatal("wrong Windows resource architecture")
			}
			section := executable.Section(".rsrc")
			if section == nil {
				t.Fatal("Windows executable has no icon resources")
			}
			data, err := section.Data()
			if err != nil {
				t.Fatal(err)
			}
			entries := int(binary.LittleEndian.Uint16(data[12:])) + int(binary.LittleEndian.Uint16(data[14:]))
			types := make(map[uint32]bool)
			for i := 0; i < entries; i++ {
				types[binary.LittleEndian.Uint32(data[16+8*i:])] = true
			}
			if !types[3] || !types[14] {
				t.Fatal("Windows executable is missing RT_ICON or RT_GROUP_ICON")
			}
			ico, err := os.ReadFile(filepath.Join("..", "..", "internal", "desktop", "assets", "icon.ico"))
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 6; i++ {
				entry := ico[6+16*i:]
				start, length := int(binary.LittleEndian.Uint32(entry[12:])), int(binary.LittleEndian.Uint32(entry[8:]))
				if !bytes.Contains(data, ico[start:start+length]) {
					t.Fatal("Windows resource is stale: run go run ./cmd/app-icon")
				}
			}
		})
	}
}
