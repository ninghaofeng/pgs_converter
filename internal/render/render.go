// Package render owns the libass/cgo boundary and composites its image list
// into cropped, Go-owned subtitle images in output-canvas coordinates.
package render

/*
#cgo pkg-config: libass
#include "bridge.h"
*/
import "C"

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

// Interval shares the millisecond event contract used by subtitle parsing.
type Interval = subtitle.Interval

// Renderer owns one native library, renderer and track. It is not safe for
// concurrent use; each conversion worker needs its own instance.
// All methods except repeated Close calls require an open renderer.
type Renderer struct {
	ptr *C.pgs_renderer
}

// New creates a renderer for a validated canvas and loads additional fonts.
// Each font directory is non-recursive; system font lookup remains enabled.
// The caller must Close a successful result, including on later load failure.
func New(width, height int, fonts []string) (*Renderer, error) {
	p := C.pgs_new(C.int(width), C.int(height))
	if p == nil {
		return nil, fmt.Errorf("libass initialization failed")
	}
	r := &Renderer{p}
	ok := false
	defer func() {
		if !ok {
			r.Close()
		}
	}()
	for _, dir := range fonts {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return nil, fmt.Errorf("font directory: %w", err)
		}
		count := 0
		for _, e := range entries {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if e.IsDir() || (ext != ".ttf" && ext != ".otf" && ext != ".ttc") {
				continue
			}
			path := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				return nil, fmt.Errorf("read font %s: %w", path, err)
			}
			if len(data) == 0 || int64(len(data)) > 1<<30 {
				return nil, fmt.Errorf("invalid font size: %s", path)
			}
			name := C.CString(e.Name())
			// ass_add_font copies the font data during the call; neither the
			// temporary Go buffer nor the C name is retained by this wrapper.
			C.ass_add_font(p.library, name, (*C.char)(unsafe.Pointer(&data[0])), C.int(len(data)))
			C.free(unsafe.Pointer(name))
			count++
		}
		if count == 0 {
			return nil, fmt.Errorf("font directory %q contains no TTF, OTF or TTC files", dir)
		}
	}
	C.ass_set_fonts(p.renderer, nil, nil, C.ASS_FONTPROVIDER_AUTODETECT, nil, 1)
	ok = true
	return r, nil
}

// Close releases native resources and is safe to repeat on the same instance.
// The renderer must not be used or closed concurrently with another operation.
func (r *Renderer) Close() {
	C.pgs_free(r.ptr)
	r.ptr = nil
}

// Diagnostics drains native warnings and selected font diagnostics.
// Normal font-family substitution may not emit a diagnostic, so this is not
// an exhaustive font-identity or glyph-coverage preflight.
func (r *Renderer) Diagnostics() []string {
	text := C.GoString(&r.ptr.diagnostics[0])
	if r.ptr.truncated != 0 {
		text += "\nlibass diagnostics truncated"
	}
	C.pgs_clear_diagnostics(r.ptr)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(text), "\n")
}

// Load parses one UTF-8 ASS/SSA script and returns events in libass track order.
// Input must be NUL-free and intervals must fit the non-wrapping SUP timeline.
// A renderer can load only one track and should be discarded after load failure.
func (r *Renderer) Load(text string) ([]Interval, error) {
	if r.ptr.track != nil {
		return nil, fmt.Errorf("renderer already has a subtitle track")
	}
	c := C.CString(text)
	defer C.free(unsafe.Pointer(c))
	if C.pgs_load(r.ptr, c, C.size_t(len(text))) == 0 {
		return nil, fmt.Errorf("libass could not parse subtitle script")
	}
	t := r.ptr.track
	if t.n_events < 1 {
		return nil, fmt.Errorf("subtitle script has no events")
	}
	events := unsafe.Slice(t.events, int(t.n_events))
	intervals := make([]Interval, len(events))
	for i, event := range events {
		start, duration := int64(event.Start), int64(event.Duration)
		if start < 0 || duration <= 0 || start > 47721858 || duration > 47721858-start {
			return nil, fmt.Errorf("event %d: invalid or out-of-range interval", i+1)
		}
		intervals[i] = Interval{Start: start, End: start + duration}
	}
	return intervals, nil
}

// SetTimes restores validated SRT millisecond times after Load and before Frame.
// Its entries must match the loaded events in number and original script order.
func (r *Renderer) SetTimes(times []Interval) error {
	t := r.ptr.track
	if len(times) != int(t.n_events) {
		return fmt.Errorf("SRT event count differs from parsed ASS event count")
	}
	events := unsafe.Slice(t.events, int(t.n_events))
	for i, v := range times {
		events[i].Start = C.longlong(v.Start)
		events[i].Duration = C.longlong(v.End - v.Start)
	}
	return nil
}

// Frame samples a loaded track at a nonnegative millisecond timestamp.
// A false change flag means "reuse the previous frame", not "clear the screen".
// A true flag with a nil image means clear; non-nil images own their pixels
// and retain canvas coordinates in Rect even after later Frame calls.
func (r *Renderer) Frame(ms int64) (*image.NRGBA, bool, error) {
	var changed C.int
	first := C.ass_render_frame(r.ptr.renderer, r.ptr.track, C.longlong(ms), &changed)
	if changed == 0 {
		return nil, false, nil
	}
	var rect image.Rectangle
	for p := first; p != nil; p = p.next {
		if p.w > 0 && p.h > 0 {
			rect = rect.Union(image.Rect(int(p.dst_x), int(p.dst_y), int(p.dst_x+p.w), int(p.dst_y+p.h)))
		}
	}
	if rect.Empty() {
		return nil, true, nil
	}
	dest := image.NewRGBA(rect)
	// libass's linked-list order is the required paint order for glyphs,
	// outlines, shadows and overlapping layers. Do not sort these images.
	for p := first; p != nil; p = p.next {
		w, h, stride := int(p.w), int(p.h), int(p.stride)
		if w <= 0 || h <= 0 {
			continue
		}
		if stride < w || p.bitmap == nil {
			return nil, false, fmt.Errorf("invalid libass image dimensions")
		}
		// Native bitmaps are borrowed until the next render. The final row
		// need not contain stride padding: copying stride*h can overread C memory.
		data := C.GoBytes(unsafe.Pointer(p.bitmap), C.int(stride*(h-1)+w))
		mask := &image.Alpha{Pix: data, Stride: stride, Rect: image.Rect(0, 0, w, h)}
		rgba := uint32(p.color)
		rr, gg, bb := colorx.ASSColor(float64(byte(rgba>>24))/255, float64(byte(rgba>>16))/255, float64(byte(rgba>>8))/255, int(r.ptr.track.YCbCrMatrix))
		// ASS's low byte is transparency (255 means invisible), whereas Go
		// uses opacity. DrawMask combines that opacity with glyph coverage.
		paint := image.NewUniform(color.NRGBA{R: byte(rr*255 + 0.5), G: byte(gg*255 + 0.5), B: byte(bb*255 + 0.5), A: 255 - byte(rgba)})
		dst := image.Rect(int(p.dst_x), int(p.dst_y), int(p.dst_x)+w, int(p.dst_y)+h)
		draw.DrawMask(dest, dst, paint, image.Point{}, mask, image.Point{}, draw.Over)
	}
	// The bitmap union may include fully transparent padding. Trim it only
	// after compositing so PGS object dimensions describe actual visible pixels.
	visible := image.Rectangle{}
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if dest.Pix[dest.PixOffset(x, y)+3] != 0 {
				visible = visible.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	if visible.Empty() {
		return nil, true, nil
	}
	out := image.NewNRGBA(visible)
	// Go's RGBA destination stores premultiplied components; NRGBA restores
	// straight colors for palette quantization while preserving coverage.
	draw.Draw(out, visible, dest, visible.Min, draw.Src)
	return out, true, nil
}
