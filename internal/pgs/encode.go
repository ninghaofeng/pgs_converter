// Package pgs writes standalone PGS/SUP streams without a native codec.
// Each visible update starts a self-contained epoch with one cropped object;
// empty compositions clear the display. This favors simple software playback
// over object-cache optimization or Blu-ray decoder-buffer scheduling.
package pgs

import (
	"encoding/binary"
	"fmt"
	"image"
	"io"
	"math"
	"sort"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
)

type sample struct {
	key uint32     // Straight source RGBA packed from most to least significant byte.
	n   int        // Pixel count used to weight palette partitioning.
	v   [4]float64 // Linear-light, premultiplied RGB followed by coverage alpha.
}

// box groups source colors during weighted median-cut palette reduction.
type box struct {
	points []sample
	weight int
}

// features makes nearly transparent colors less influential than opaque ones.
// Alpha remains a separate dimension so coverage differences are not discarded.
func features(k uint32) [4]float64 {
	a := float64(byte(k)) / 255
	return [4]float64{colorx.Linear(float64(byte(k>>24))/255) * a,
		colorx.Linear(float64(byte(k>>16))/255) * a,
		colorx.Linear(float64(byte(k>>8))/255) * a, a}
}

func keyAt(im *image.NRGBA, x, y int) uint32 {
	i := im.PixOffset(x, y)
	if im.Pix[i+3] == 0 {
		// Invisible RGB values have no visual meaning and share palette index 0.
		return 0
	}
	return uint32(im.Pix[i])<<24 | uint32(im.Pix[i+1])<<16 | uint32(im.Pix[i+2])<<8 | uint32(im.Pix[i+3])
}

func spread(b box) (axis int, value float64) {
	for c := 0; c < 4; c++ {
		lo, hi := 1.0, 0.0
		for _, p := range b.points {
			lo = math.Min(lo, p.v[c])
			hi = math.Max(hi, p.v[c])
		}
		if hi-lo > value {
			axis, value = c, hi-lo
		}
	}
	return
}

// quantize returns tightly packed, row-major indices and a source-RGBA palette.
// Index 0 is always transparent, leaving at most 255 nontransparent colors.
// Profiles are applied later so SDR and HDR retain identical indices and alpha.
func quantize(im *image.NRGBA) ([]byte, []uint32) {
	hist := make(map[uint32]int)
	for y := im.Rect.Min.Y; y < im.Rect.Max.Y; y++ {
		for x := im.Rect.Min.X; x < im.Rect.Max.X; x++ {
			k := keyAt(im, x, y)
			if k != 0 {
				hist[k]++
			}
		}
	}
	keys := make([]uint32, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	// Histogram iteration is randomized; sorting stabilizes exact palettes,
	// partition tie-breaking and therefore the complete encoded stream.
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	palette := []uint32{0}
	mapping := map[uint32]byte{0: 0}
	if len(keys) <= 255 {
		for _, k := range keys {
			mapping[k] = byte(len(palette))
			palette = append(palette, k)
		}
	} else {
		points := make([]sample, len(keys))
		total := 0
		for i, k := range keys {
			points[i] = sample{k, hist[k], features(k)}
			total += hist[k]
		}
		boxes := []box{{points, total}}
		for len(boxes) < 255 {
			best, axis, score := -1, 0, -1.0
			for i, b := range boxes {
				if len(b.points) < 2 {
					continue
				}
				c, s := spread(b)
				// Favor wide, populated boxes without letting population alone
				// consume every palette entry in common flat colors.
				s *= math.Sqrt(float64(b.weight))
				if s > score {
					best, axis, score = i, c, s
				}
			}
			if best < 0 {
				break
			}
			b := boxes[best]
			sort.Slice(b.points, func(i, j int) bool {
				a, z := b.points[i], b.points[j]
				if a.v[axis] == z.v[axis] {
					return a.key < z.key
				}
				return a.v[axis] < z.v[axis]
			})
			split, weight := 1, b.points[0].n
			// Split at the weighted median, keeping both children nonempty.
			for split < len(b.points)-1 && weight < b.weight/2 {
				weight += b.points[split].n
				split++
			}
			boxes[best] = box{b.points[:split], weight}
			boxes = append(boxes, box{b.points[split:], b.weight - weight})
		}
		for _, b := range boxes {
			// Use an actual sample nearest the weighted centroid, avoiding an
			// extra RGB quantization stage before the HDR palette transform.
			var centroid [4]float64
			for _, p := range b.points {
				for c := range centroid {
					centroid[c] += p.v[c] * float64(p.n) / float64(b.weight)
				}
			}
			best, distance := b.points[0].key, math.Inf(1)
			for _, p := range b.points {
				d := 0.0
				for c := range centroid {
					d += (p.v[c] - centroid[c]) * (p.v[c] - centroid[c])
				}
				if d < distance {
					best, distance = p.key, d
				}
			}
			id := byte(len(palette))
			palette = append(palette, best)
			for _, p := range b.points {
				mapping[p.key] = id
			}
		}
	}
	pixels := make([]byte, im.Rect.Dx()*im.Rect.Dy())
	i := 0
	for y := im.Rect.Min.Y; y < im.Rect.Max.Y; y++ {
		for x := im.Rect.Min.X; x < im.Rect.Max.X; x++ {
			pixels[i] = mapping[keyAt(im, x, y)]
			i++
		}
	}
	return pixels, palette
}

// RLE encodes tightly packed palette indices in PGS row order.
// Width must be positive and divide len(pixels); callers validate dimensions.
// Runs never cross scanlines and are split at the 14-bit run-length limit.
func RLE(pixels []byte, width int) []byte {
	var out []byte
	for row := 0; row < len(pixels); row += width {
		for x := 0; x < width; {
			c := pixels[row+x]
			n := 1
			for x+n < width && pixels[row+x+n] == c && n < 0x3fff {
				n++
			}
			if c != 0 && n <= 2 {
				// Nonzero bytes are literal pixels; very short runs need no header.
				for i := 0; i < n; i++ {
					out = append(out, c)
				}
			} else {
				// Zero introduces a control code: bit 7 adds an explicit color,
				// bit 6 extends the length to 14 bits. Otherwise the color is 0.
				flag := byte(0)
				if c != 0 {
					flag = 0x80
				}
				out = append(out, 0)
				if n < 64 {
					out = append(out, flag|byte(n))
				} else {
					out = append(out, flag|0x40|byte(n>>8), byte(n))
				}
				if c != 0 {
					out = append(out, c)
				}
			}
			x += n
		}
		out = append(out, 0, 0) // A zero-length control code terminates the row.
	}
	return out
}

// Encoder tracks one stream's canvas, composition counter and last timestamp.
// It is not concurrency-safe. After any write error, discard the encoder and
// its output: the writer may already contain an incomplete display set.
type Encoder struct {
	w             io.Writer
	width, height int
	rate          byte
	profile       colorx.Profile
	number        uint16
	last          int64
}

// New validates the canvas and creates an encoder without writing a header.
// Rate must be a PGS frame-rate code and profile must already be validated.
// The area cap bounds full-canvas work; it is not a hardware-compliance claim.
func New(w io.Writer, width, height int, rate byte, profile colorx.Profile) (*Encoder, error) {
	if width < 1 || height < 1 || width > 65535 || height > 65535 || int64(width)*int64(height) > 3840*2160 {
		return nil, fmt.Errorf("canvas must fit PGS fields and contain at most 3840x2160 pixels")
	}
	return &Encoder{w: w, width: width, height: height, rate: rate, profile: profile, last: -1}, nil
}

func u16(b []byte, n int) []byte { return binary.BigEndian.AppendUint16(b, uint16(n)) }

// segment writes a 13-byte SUP envelope followed by one segment payload.
// All multibyte fields are big-endian. DTS is zero because this encoder does
// not schedule predecoding against the Blu-ray decoder-buffer model.
func (e *Encoder) segment(pts int64, typ byte, data []byte) error {
	if len(data) > 65535 {
		return fmt.Errorf("PGS segment too large: %d", len(data))
	}
	header := []byte{'P', 'G'}
	header = binary.BigEndian.AppendUint32(header, uint32(pts))
	header = binary.BigEndian.AppendUint32(header, 0)
	header = append(header, typ)
	header = u16(header, len(data))
	if n, err := e.w.Write(header); err != nil {
		return err
	} else if n != len(header) {
		return io.ErrShortWrite
	}
	if n, err := e.w.Write(data); err != nil {
		return err
	} else if n != len(data) {
		return io.ErrShortWrite
	}
	return nil
}

// Write emits one complete display set at a strictly increasing 90 kHz PTS.
// A nil image clears the display; otherwise Rect supplies canvas placement,
// not just image dimensions. PTS wrapping and out-of-canvas objects are errors.
func (e *Encoder) Write(pts int64, im *image.NRGBA) error {
	if pts < 0 || pts > math.MaxUint32 || pts <= e.last {
		return fmt.Errorf("PGS timestamp out of range or not increasing: %d", pts)
	}
	if im != nil && (im.Rect.Empty() || !im.Rect.In(image.Rect(0, 0, e.width, e.height))) {
		return fmt.Errorf("subtitle rectangle outside canvas: %v", im.Rect)
	}
	// PCS (0x16): video descriptor, composition descriptor and object refs.
	// Epoch starts reset object/palette state, so IDs and versions can stay zero.
	pcs := u16(nil, e.width)
	pcs = u16(pcs, e.height)
	pcs = append(pcs, e.rate)
	pcs = u16(pcs, int(e.number))
	state, objects := byte(0), byte(0)
	if im != nil {
		state, objects = 0x80, 1
	}
	pcs = append(pcs, state, 0, 0, objects)
	if im != nil {
		// Object ID 0, window ID 0, no cropping/forced-display flags.
		pcs = append(pcs, 0, 0, 0, 0)
		pcs = u16(pcs, im.Rect.Min.X)
		pcs = u16(pcs, im.Rect.Min.Y)
	}
	if err := e.segment(pts, 0x16, pcs); err != nil {
		return err
	}
	if im != nil {
		// WDS (0x17) declares one window matching the cropped image rectangle.
		window := []byte{1, 0}
		window = u16(window, im.Rect.Min.X)
		window = u16(window, im.Rect.Min.Y)
		window = u16(window, im.Rect.Dx())
		window = u16(window, im.Rect.Dy())
		if err := e.segment(pts, 0x17, window); err != nil {
			return err
		}
		pixels, palette := quantize(im)
		// PDS (0x14): palette ID/version, then index/Y/Cr/Cb/alpha entries.
		// Stored alpha is straight coverage and is not adjusted for HDR white.
		pds := []byte{0, 0}
		for i, k := range palette {
			y, cr, cb := e.profile.Palette(float64(byte(k>>24))/255, float64(byte(k>>16))/255, float64(byte(k>>8))/255)
			pds = append(pds, byte(i), y, cr, cb, byte(k))
		}
		if err := e.segment(pts, 0x14, pds); err != nil {
			return err
		}
		rle := RLE(pixels, im.Rect.Dx())
		// ODS object_data_length includes width/height (four bytes), but
		// excludes its own three-byte length field and fragment headers.
		length := len(rle) + 4
		if length > 0xffffff {
			return fmt.Errorf("PGS object exceeds 24-bit length limit")
		}
		first := true
		for len(rle) > 0 {
			// Each ODS (0x15) fragment repeats object ID/version/sequence flags.
			// Only the first fragment carries total length and dimensions.
			ods := []byte{0, 0, 0, 0}
			if first {
				ods[3] = 0x80
				ods = append(ods, byte(length>>16), byte(length>>8), byte(length))
				ods = u16(ods, im.Rect.Dx())
				ods = u16(ods, im.Rect.Dy())
			}
			// The 16-bit SUP payload limit includes this fragment's ODS header.
			n := min(len(rle), 65535-len(ods))
			if n == len(rle) {
				ods[3] |= 0x40
			}
			ods = append(ods, rle[:n]...)
			if err := e.segment(pts, 0x15, ods); err != nil {
				return err
			}
			rle, first = rle[n:], false
		}
	}
	// END (0x80) commits the display set, including an object-free clear.
	if err := e.segment(pts, 0x80, nil); err != nil {
		return err
	}
	e.number++
	e.last = pts
	return nil
}
