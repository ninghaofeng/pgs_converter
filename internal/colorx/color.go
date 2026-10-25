// Package colorx converts subtitle RGB colors into PGS palette values.
// It separates ASS compatibility adjustments from output encoding: ASS colors
// first target BT.709 SDR graphics, then optionally become BT.2020/PQ colors.
// Alpha is coverage, not luminance, and is never transformed by this package.
package colorx

import (
	"fmt"
	"math"
)

// Profile describes the intended interpretation of an output palette.
// It does not add HDR signaling to SUP or control a player's compositing.
type Profile struct {
	HDR bool
	// White maps source RGB white to absolute luminance in nits in HDR mode.
	// SDR leaves source RGB unchanged and does not use this value.
	White float64
}

// New accepts "sdr" or experimental "hdr10" and validates reference white.
// White must be finite and in (0, 10000], even when selecting SDR.
func New(name string, white float64) (Profile, error) {
	if name != "sdr" && name != "hdr10" {
		return Profile{}, fmt.Errorf("unknown color profile %q (use sdr or hdr10)", name)
	}
	if math.IsNaN(white) || math.IsInf(white, 0) || white <= 0 || white > 10000 {
		return Profile{}, fmt.Errorf("subtitle white must be finite and in (0, 10000] nits")
	}
	return Profile{HDR: name == "hdr10", White: white}, nil
}

// Linear removes the sRGB transfer function from a normalized component.
// Callers supply values in [0, 1]; the function does not clamp its input.
func Linear(v float64) float64 {
	if v <= 0.04045 {
		return v / 12.92
	}
	return math.Pow((v+0.055)/1.055, 2.4)
}

// PQ encodes absolute luminance with SMPTE ST 2084, clamping to 0..10000 nits.
// The constants are the exact rational parameters of the PQ transfer function.
func PQ(nits float64) float64 {
	const m1, m2 = 2610.0 / 16384, 2523.0 / 32
	const c1, c2, c3 = 3424.0 / 4096, 2413.0 / 128, 2392.0 / 128
	l := math.Pow(math.Max(0, math.Min(nits, 10000))/10000, m1)
	return math.Pow((c1+c2*l)/(1+c3*l), m2)
}

// UnPQ decodes a normalized ST 2084 signal into absolute luminance in nits.
// Its input must be in [0, 1]; it is also used by numerical reference tests.
func UnPQ(v float64) float64 {
	const m1, m2 = 2610.0 / 16384, 2523.0 / 32
	const c1, c2, c3 = 3424.0 / 4096, 2413.0 / 128, 2392.0 / 128
	p := math.Pow(v, 1/m2)
	return 10000 * math.Pow(math.Max(p-c1, 0)/(c2-c3*p), 1/m1)
}

// RGB takes straight, normalized sRGB components and returns output-domain RGB.
// SDR is an identity transform. HDR returns nonlinear PQ components after
// linear-light primary conversion and absolute reference-white scaling.
func (p Profile) RGB(r, g, b float64) (float64, float64, float64) {
	if !p.HDR {
		return r, g, b
	}
	r, g, b = Linear(r), Linear(g), Linear(b)
	// Both color spaces use D65 white. Apply the BT.709-to-BT.2020
	// primary matrix before PQ; multiplying PQ signal values is not valid.
	return PQ(p.White * (0.627403896*r + 0.329283038*g + 0.043313066*b)),
		PQ(p.White * (0.069097289*r + 0.919540395*g + 0.011362316*b)),
		PQ(p.White * (0.016391439*r + 0.088013308*g + 0.895595253*b))
}

func byteValue(v float64) byte {
	return byte(math.Round(math.Max(0, math.Min(255, v))))
}

// Palette encodes straight source RGB as limited-range, 8-bit PGS Y/Cr/Cb.
// The return order matches PDS wire order, which places Cr before Cb.
// Transparency is written separately by the PGS encoder.
func (p Profile) Palette(r, g, b float64) (y, cr, cb byte) {
	r, g, b = p.RGB(r, g, b)
	kr, kb := 0.2126, 0.0722
	if p.HDR {
		kr, kb = 0.2627, 0.0593
	}
	l := kr*r + (1-kr-kb)*g + kb*b
	// Nominal luma spans 16..235; chroma is centered at 128 with a
	// 224-code excursion. Quantize only after color and transfer conversion.
	return byteValue(16 + 219*l), byteValue(128 + 112*(r-l)/(1-kr)), byteValue(128 + 112*(b-l)/(1-kb))
}

// ASSColor reproduces a script's YCbCr values on a BT.709 limited-range target.
// Matrix is the numeric ASS_YCbCrMatrix enum from libass/ass_types.h; missing
// or unknown headers follow TV.601, while None bypasses compatibility mangling.
// This is legacy ASS color matching, not a conversion of video HDR metadata.
func ASSColor(r, g, b float64, matrix int) (float64, float64, float64) {
	if matrix == 2 { // YCbCr Matrix: None
		return r, g, b
	}
	kr, kb := 0.299, 0.114
	switch matrix {
	case 5, 6: // BT.709 TV/PC
		kr, kb = 0.2126, 0.0722
	case 7, 8: // SMPTE 240M TV/PC
		kr, kb = 0.212, 0.087
	case 9, 10: // FCC TV/PC
		kr, kb = 0.30, 0.11
	}
	y := kr*r + (1-kr-kb)*g + kb*b
	cb, cr := (b-y)/(2*(1-kb)), (r-y)/(2*(1-kr))
	// PC variants encode full-range values that are then interpreted by
	// the limited-range target; preserve that historical mismatch.
	if matrix == 4 || matrix == 6 || matrix == 8 || matrix == 10 {
		y, cb, cr = (255*y-16)/219, cb*255/224, cr*255/224
	}
	r, b = y+2*(1-0.2126)*cr, y+2*(1-0.0722)*cb
	g = (y - 0.2126*r - 0.0722*b) / (1 - 0.2126 - 0.0722)
	clamp := func(v float64) float64 { return math.Max(0, math.Min(1, v)) }
	return clamp(r), clamp(g), clamp(b)
}
