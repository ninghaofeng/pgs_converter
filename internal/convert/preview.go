package convert

import (
	"context"
	"fmt"
	"image"
	"image/draw"
	"os"
	"path/filepath"
	"strings"

	"github.com/ninghaofeng/pgs_converter/internal/render"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

func readSource(path string, opt Options) (string, []subtitle.Interval, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", nil, err
	}
	text, err := subtitle.Decode(data, opt.Encoding)
	if err != nil {
		return "", nil, err
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".srt":
		doc, err := subtitle.SRT(text, opt.Width, opt.Height, opt.Style)
		if err != nil {
			return "", nil, err
		}
		return doc.Text, doc.Times, nil
	case ".ass", ".ssa":
		return text, nil, nil
	default:
		return "", nil, fmt.Errorf("unsupported subtitle extension: %s", filepath.Ext(path))
	}
}

type PreviewResult struct {
	Image     *image.NRGBA
	Intervals []subtitle.Interval
	Warnings  []string
}

// Preview renders one exact millisecond using export's source preparation and
// libass. It is an SDR style preview, before SUP quantization and HDR encoding.
// Each call owns its renderer, so previews cannot race with conversion workers.
func Preview(ctx context.Context, path string, opt Options, ms int64) (PreviewResult, error) {
	var result PreviewResult
	if ms < 0 || ms > 47721858 {
		return result, fmt.Errorf("preview timestamp must be in 0..47721858 ms")
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := opt.Validate(); err != nil {
		return result, err
	}
	text, times, err := readSource(path, opt)
	if err != nil {
		return result, err
	}
	r, err := render.New(opt.Width, opt.Height, opt.Fonts)
	if err != nil {
		return result, err
	}
	defer r.Close()
	result.Intervals, err = r.Load(text)
	if err == nil && times != nil {
		err = r.SetTimes(times)
		result.Intervals = times
	}
	if err != nil {
		return result, err
	}
	im, _, err := r.Frame(ms)
	result.Warnings = r.Diagnostics()
	if err != nil {
		return result, err
	}
	if opt.Strict && len(result.Warnings) > 0 {
		return result, fmt.Errorf("strict mode: %s", result.Warnings[0])
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	result.Image = image.NewNRGBA(image.Rect(0, 0, opt.Width, opt.Height))
	if im != nil {
		draw.Draw(result.Image, im.Bounds(), im, im.Bounds().Min, draw.Src)
	}
	return result, nil
}
