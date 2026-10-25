// Command app-icon regenerates application icon assets from the source SVG.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/fyne-io/oksvg"
	"github.com/srwiley/rasterx"
)

func renderPNG(source []byte, size int) ([]byte, error) {
	icon, err := oksvg.ReadIconStream(bytes.NewReader(source), oksvg.StrictErrorMode)
	if err != nil {
		return nil, err
	}
	im := image.NewRGBA(image.Rect(0, 0, size, size))
	icon.SetTarget(0, 0, float64(size), float64(size))
	icon.Draw(rasterx.NewDasher(size, size, rasterx.NewScannerGV(size, size, im, im.Bounds())), 1)
	var data bytes.Buffer
	if err := png.Encode(&data, im); err != nil {
		return nil, err
	}
	return data.Bytes(), nil
}

func icons(source []byte) (map[string][]byte, error) {
	sizes := []int{16, 32, 48, 64, 128, 256, 512, 1024}
	images := make(map[int][]byte)
	for _, size := range sizes {
		data, err := renderPNG(source, size)
		if err != nil {
			return nil, err
		}
		images[size] = data
	}
	// ICO directories point to PNG payloads; zero width/height represents 256.
	windowsSizes := sizes[:6]
	ico := make([]byte, 6+16*len(windowsSizes))
	binary.LittleEndian.PutUint16(ico[2:], 1)
	binary.LittleEndian.PutUint16(ico[4:], uint16(len(windowsSizes)))
	for i, size := range windowsSizes {
		entry := ico[6+16*i:]
		entry[0], entry[1] = byte(size%256), byte(size%256)
		binary.LittleEndian.PutUint16(entry[4:], 1)
		binary.LittleEndian.PutUint16(entry[6:], 32)
		binary.LittleEndian.PutUint32(entry[8:], uint32(len(images[size])))
		binary.LittleEndian.PutUint32(entry[12:], uint32(len(ico)))
		ico = append(ico, images[size]...)
	}
	icns := []byte("icns\x00\x00\x00\x00")
	for _, entry := range []struct {
		tag  string
		size int
	}{{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128}, {"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
		{"ic11", 32}, {"ic12", 64}, {"ic13", 256}, {"ic14", 512}} {
		header := make([]byte, 8)
		copy(header, entry.tag)
		binary.BigEndian.PutUint32(header[4:], uint32(8+len(images[entry.size])))
		icns = append(icns, header...)
		icns = append(icns, images[entry.size]...)
	}
	binary.BigEndian.PutUint32(icns[4:], uint32(len(icns)))
	return map[string][]byte{"icon.png": images[512], "icon.ico": ico, "icon.icns": icns}, nil
}

func generate() error {
	assets := filepath.Join("internal", "desktop", "assets")
	source, err := os.ReadFile(filepath.Join(assets, "icon.svg"))
	if err != nil {
		return err
	}
	output, err := icons(source)
	if err != nil {
		return err
	}
	for name, data := range output {
		if err := os.WriteFile(filepath.Join(assets, name), data, 0644); err != nil {
			return err
		}
	}
	for _, arch := range []string{"amd64", "arm64"} {
		target := filepath.Join("cmd", "pgs-converter-gui", "icon_windows_"+arch+".syso")
		cmd := exec.Command("go", "run", "github.com/akavel/rsrc@v0.10.2", "-arch", arch,
			"-ico", filepath.Join(assets, "icon.ico"), "-o", target)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("generate Windows %s icon: %w: %s", arch, err, output)
		}
	}
	return nil
}

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, "app-icon:", err)
		os.Exit(1)
	}
}
