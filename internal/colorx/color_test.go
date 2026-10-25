package colorx

import (
	"math"
	"testing"
)

func TestPQReference(t *testing.T) {
	// Absolute reference vectors prevent a mutually consistent but incorrect
	// PQ/UnPQ pair from passing on roundtrip checks alone.
	for _, tc := range []struct{ nits, code float64 }{
		{0, 0.0000007309559026}, {100, 0.5080784215},
		{203, 0.5806888810}, {300, 0.6218628370}, {10000, 1},
	} {
		if got := PQ(tc.nits); math.Abs(got-tc.code) > 1e-8 {
			t.Errorf("PQ(%g)=%g, want %g", tc.nits, got, tc.code)
		}
		if got := UnPQ(PQ(tc.nits)); math.Abs(got-tc.nits) > 1e-6 {
			t.Errorf("roundtrip %g: %g", tc.nits, got)
		}
	}
}

func TestPalette(t *testing.T) {
	for _, name := range []string{"sdr", "hdr10"} {
		p, err := New(name, 203)
		if err != nil {
			t.Fatal(err)
		}
		y, cr, cb := p.Palette(0, 0, 0)
		if y != 16 || cb != 128 || cr != 128 {
			t.Fatalf("black %s: %d %d %d", name, y, cb, cr)
		}
		y, cr, cb = p.Palette(1, 1, 1)
		want := byte(235)
		if p.HDR {
			want = 143 // round(16 + 219 * PQ(203)).
		}
		if y != want || cr != 128 || cb != 128 {
			t.Fatalf("white %s: %d %d %d", name, y, cr, cb)
		}
	}
	// Check a chromatic primary as well as neutral white: white alone cannot
	// detect a missing or incorrect BT.709-to-BT.2020 primary transform.
	p, _ := New("hdr10", 203)
	r, g, b := p.RGB(1, 0, 0)
	// Linear BT.2020 values for sRGB red, before PQ encoding.
	for i, tc := range []struct{ got, want float64 }{
		{UnPQ(r) / 203, 0.627403896},
		{UnPQ(g) / 203, 0.069097289},
		{UnPQ(b) / 203, 0.016391439},
	} {
		if math.Abs(tc.got-tc.want) > 1e-8 {
			t.Errorf("red primary component %d: %g want %g", i, tc.got, tc.want)
		}
	}
}

func TestProfileValidation(t *testing.T) {
	for _, n := range []float64{0, -1, 10001, math.NaN(), math.Inf(1)} {
		if _, err := New("hdr10", n); err == nil {
			t.Errorf("accepted %g", n)
		}
	}
	if _, err := New("hlg", 203); err == nil {
		t.Fatal("accepted unsupported profile")
	}
}
