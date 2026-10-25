package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
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
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(assets, name))
			if err != nil {
				t.Fatal(err)
			}
			gotImages, wantImages := iconPNGs(t, name, data), iconPNGs(t, name, want)
			for i := range wantImages {
				if err := comparePNG(gotImages[i], wantImages[i]); err != nil {
					t.Fatalf("%s image %d is stale: run go run ./cmd/app-icon: %v", name, i, err)
				}
			}
		})
	}
	im, err := png.Decode(bytes.NewReader(generated["icon.png"]))
	if err != nil {
		t.Fatal(err)
	}
	if im.Bounds().Dx() != 512 || im.Bounds().Dy() != 512 {
		t.Fatal("wrong application icon dimensions")
	}
	// A valid PNG can still contain only the background if a transform collapses
	// the glyph. Check visible content rather than format and dimensions alone.
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
}

// PNG compression can change between Go versions without changing any pixels.
func comparePNG(got, want []byte) error {
	actual, err := png.Decode(bytes.NewReader(got))
	if err != nil {
		return fmt.Errorf("decode actual PNG: %w", err)
	}
	expected, err := png.Decode(bytes.NewReader(want))
	if err != nil {
		return fmt.Errorf("decode expected PNG: %w", err)
	}
	if actual.Bounds() != expected.Bounds() {
		return fmt.Errorf("PNG dimensions differ: got %v, want %v", actual.Bounds(), expected.Bounds())
	}
	for y := expected.Bounds().Min.Y; y < expected.Bounds().Max.Y; y++ {
		for x := expected.Bounds().Min.X; x < expected.Bounds().Max.X; x++ {
			r, g, b, a := actual.At(x, y).RGBA()
			wr, wg, wb, wa := expected.At(x, y).RGBA()
			if r != wr || g != wg || b != wb || a != wa {
				return fmt.Errorf("PNG pixel differs at (%d, %d)", x, y)
			}
		}
	}
	return nil
}

func iconPNGs(t *testing.T, name string, data []byte) [][]byte {
	t.Helper()
	var images [][]byte
	var sizes []int
	switch name {
	case "icon.png":
		images, sizes = [][]byte{data}, []int{512}
	case "icon.ico":
		sizes = []int{16, 32, 48, 64, 128, 256}
		offset := 6 + 16*len(sizes)
		if len(data) < offset || binary.LittleEndian.Uint16(data) != 0 ||
			binary.LittleEndian.Uint16(data[2:]) != 1 ||
			int(binary.LittleEndian.Uint16(data[4:])) != len(sizes) {
			t.Fatal("invalid ICO header or resolution count")
		}
		for i, size := range sizes {
			entry := data[6+16*i : 6+16*(i+1)]
			length := uint64(binary.LittleEndian.Uint32(entry[8:]))
			start := uint64(binary.LittleEndian.Uint32(entry[12:]))
			if entry[0] != byte(size%256) || entry[1] != byte(size%256) ||
				entry[2] != 0 || entry[3] != 0 ||
				binary.LittleEndian.Uint16(entry[4:]) != 1 ||
				binary.LittleEndian.Uint16(entry[6:]) != 32 ||
				start != uint64(offset) || length == 0 || start+length > uint64(len(data)) {
				t.Fatalf("invalid ICO directory entry %d", i)
			}
			offset += int(length)
			images = append(images, data[int(start):offset])
		}
		if offset != len(data) {
			t.Fatal("unexpected trailing ICO data")
		}
	case "icon.icns":
		tags := []string{"icp4", "icp5", "icp6", "ic07", "ic08", "ic09", "ic10", "ic11", "ic12", "ic13", "ic14"}
		sizes = []int{16, 32, 64, 128, 256, 512, 1024, 32, 64, 256, 512}
		if len(data) < 8 || string(data[:4]) != "icns" || uint64(binary.BigEndian.Uint32(data[4:])) != uint64(len(data)) {
			t.Fatal("invalid ICNS header")
		}
		offset := 8
		for _, tag := range tags {
			if len(data)-offset < 8 {
				t.Fatalf("missing ICNS entry %s", tag)
			}
			length := uint64(binary.BigEndian.Uint32(data[offset+4:]))
			if string(data[offset:offset+4]) != tag || length <= 8 || length > uint64(len(data)-offset) {
				t.Fatalf("invalid ICNS entry %s", tag)
			}
			images = append(images, data[offset+8:offset+int(length)])
			offset += int(length)
		}
		if offset != len(data) {
			t.Fatal("unexpected trailing ICNS data")
		}
	default:
		t.Fatalf("unknown icon format %q", name)
	}
	for i, payload := range images {
		config, err := png.DecodeConfig(bytes.NewReader(payload))
		if err != nil || config.Width != sizes[i] || config.Height != sizes[i] {
			t.Fatalf("invalid %s image %d: expected %dx%d PNG: %v", name, i, sizes[i], sizes[i], err)
		}
	}
	return images
}

func TestComparePNG(t *testing.T) {
	encode := func(im image.Image, compression png.CompressionLevel) []byte {
		t.Helper()
		var output bytes.Buffer
		encoder := png.Encoder{CompressionLevel: compression}
		if err := encoder.Encode(&output, im); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}
	im := image.NewRGBA(image.Rect(0, 0, 16, 16))
	im.SetRGBA(0, 0, color.RGBA{R: 255, A: 255})
	want := encode(im, png.DefaultCompression)
	alternate := encode(im, png.NoCompression)
	if bytes.Equal(want, alternate) {
		t.Fatal("fixture must use different PNG encodings")
	}
	if err := comparePNG(alternate, want); err != nil {
		t.Fatal("identical pixels should match despite different compression:", err)
	}
	im.SetRGBA(0, 0, color.RGBA{B: 255, A: 255})
	for name, data := range map[string][]byte{
		"changed-pixel": encode(im, png.DefaultCompression),
		"wrong-size":    encode(image.NewRGBA(image.Rect(0, 0, 8, 8)), png.DefaultCompression),
		"invalid-png":   []byte("not a PNG"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := comparePNG(data, want); err == nil {
				t.Fatal("invalid or changed image should not match")
			}
		})
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
			// A minimal pure-Go executable tests real COFF linking on either
			// architecture without requiring Windows Fyne/cgo graphics libraries.
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
