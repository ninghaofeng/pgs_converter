package pgs

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"math"
	"testing"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
)

// decodeRLE is a test-only reader that checks scanline boundaries as well as
// pixel equality. Independent FFmpeg tests cover full-stream interoperability.
func decodeRLE(data []byte, width, height int) ([]byte, error) {
	var out []byte
	row := 0
	for i := 0; i < len(data); {
		c := data[i]
		i++
		if c != 0 {
			out = append(out, c)
			continue
		}
		if i >= len(data) {
			return nil, fmt.Errorf("truncated run")
		}
		flags := data[i]
		i++
		if flags == 0 {
			row++
			if len(out) != row*width {
				return nil, fmt.Errorf("bad row %d length %d", row, len(out))
			}
			continue
		}
		n := int(flags & 63)
		if flags&64 != 0 {
			if i >= len(data) {
				return nil, fmt.Errorf("truncated long run")
			}
			n = n*256 + int(data[i])
			i++
		}
		c = 0
		if flags&128 != 0 {
			if i >= len(data) {
				return nil, fmt.Errorf("missing color")
			}
			c = data[i]
			i++
		}
		out = append(out, bytes.Repeat([]byte{c}, n)...)
	}
	if row != height {
		return nil, fmt.Errorf("row count %d", row)
	}
	return out, nil
}

func TestRLE(t *testing.T) {
	// Exercise literal/run choices, 6-bit and 14-bit limits, and splitting a
	// maximum-width row without allowing a run to absorb the following row.
	for _, width := range []int{1, 2, 3, 63, 64, 255, 256, 16383, 16384, 65535} {
		for _, c := range []byte{0, 1, 255} {
			pixels := bytes.Repeat([]byte{c}, width*2)
			got, err := decodeRLE(RLE(pixels, width), width, 2)
			if err != nil || !bytes.Equal(got, pixels) {
				t.Fatalf("width=%d color=%d: %v", width, c, err)
			}
		}
	}
}

func TestSegmentsAndFragmentation(t *testing.T) {
	var buf bytes.Buffer
	e, err := New(&buf, 1920, 1080, 0x20, colorx.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	// Changing each pixel defeats long runs and forces multiple ODS fragments.
	// The nonzero origin separately exercises placement versus object dimensions.
	im := image.NewNRGBA(image.Rect(5, 10, 1005, 210))
	for y := 10; y < 210; y++ {
		for x := 5; x < 1005; x++ {
			c := byte(x%254 + 1)
			im.SetNRGBA(x, y, color.NRGBA{c, c, c, 255})
		}
	}
	if err := e.Write(90000, im); err != nil {
		t.Fatal(err)
	}
	if err := e.Write(180000, nil); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	var types []byte
	var rle []byte
	odsCount, ended := 0, false
	for len(data) > 0 {
		if len(data) < 13 || string(data[:2]) != "PG" {
			t.Fatal("invalid SUP header")
		}
		n := int(binary.BigEndian.Uint16(data[11:13]))
		if n > 65535 || len(data) < n+13 {
			t.Fatal("invalid segment length")
		}
		typ, payload := data[10], data[13:13+n]
		types = append(types, typ)
		switch typ {
		case 0x15:
			start := 4
			if odsCount == 0 {
				if payload[3]&0x80 == 0 {
					t.Fatal("missing first fragment flag")
				}
				start = 11
			}
			rle = append(rle, payload[start:]...)
			ended = payload[3]&0x40 != 0
			odsCount++
		case 0x16:
			if binary.BigEndian.Uint32(data[2:6]) == 180000 && payload[10] != 0 {
				t.Fatal("clear composition has objects")
			}
		}
		data = data[13+n:]
	}
	if odsCount < 2 || !ended {
		t.Fatalf("ODS fragmentation missing: %d %v", odsCount, ended)
	}
	if _, err := decodeRLE(rle, 1000, 200); err != nil {
		t.Fatal(err)
	}
	if types[0] != 0x16 || types[len(types)-1] != 0x80 {
		t.Fatal("invalid segment sequence")
	}
	for _, pts := range []int64{180000, -1, math.MaxUint32 + 1} {
		if err := e.Write(pts, nil); err == nil {
			t.Errorf("accepted timestamp %d", pts)
		}
	}
}

func TestQuantize(t *testing.T) {
	im := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			im.SetNRGBA(x, y, color.NRGBA{byte(x * 8), byte(y * 8), 150, byte((x + y) * 4)})
		}
	}
	a, ap := quantize(im)
	b, bp := quantize(im)
	if !bytes.Equal(a, b) || len(ap) > 256 || fmt.Sprint(ap) != fmt.Sprint(bp) || a[0] != 0 {
		t.Fatal("non-deterministic or invalid quantization")
	}
	total, peak := 0.0, 0.0
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			source := features(keyAt(im, x, y))
			decoded := features(ap[a[y*32+x]])
			for c := range source {
				err := math.Abs(source[c] - decoded[c])
				total += err
				peak = math.Max(peak, err)
			}
		}
	}
	// These are regression limits for this fixture in the quantizer's feature
	// domain, not a universal perceptual-quality guarantee or YCbCr error bound.
	if mean := total / (32 * 32 * 4); mean > 0.025 || peak > 0.15 {
		t.Fatalf("linear premultiplied RGBA quantization error: mean=%g peak=%g", mean, peak)
	}
	im = image.NewNRGBA(image.Rect(0, 0, 2, 1))
	im.SetNRGBA(0, 0, color.NRGBA{255, 128, 64, 128})
	a, ap = quantize(im)
	if ap[a[0]] != 0xff804080 || a[1] != 0 {
		t.Fatal("exact palette/alpha roundtrip failed")
	}
}

func FuzzRLE(f *testing.F) {
	f.Add([]byte{0, 1, 2, 0, 0})
	f.Fuzz(func(t *testing.T, pixels []byte) {
		// Keep each fuzz case within a single representable PGS row.
		if len(pixels) == 0 || len(pixels) > 65535 {
			return
		}
		got, err := decodeRLE(RLE(pixels, len(pixels)), len(pixels), 1)
		if err != nil || !bytes.Equal(got, pixels) {
			t.Fatalf("roundtrip: %v", err)
		}
	})
}
