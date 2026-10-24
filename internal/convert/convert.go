// Package convert connects subtitle parsing, frame sampling, native rendering
// and PGS encoding. Stream exposes partial-output semantics; File adds staged
// output and commit rules for command-line conversions.
package convert

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
	"github.com/ninghaofeng/pgs_converter/internal/pgs"
	"github.com/ninghaofeng/pgs_converter/internal/render"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

// Options is read-only configuration shared by independent file conversions.
// Width/Height are canvas pixels; Rate is an exact supported frame rate.
// Style applies only to SRT, while Fonts augments system lookup for all inputs.
type Options struct {
	Width, Height int
	Rate          subtitle.Rate
	Profile       colorx.Profile
	Encoding      string
	Fonts         []string
	Style         subtitle.Style
	Strict        bool
	Overwrite     bool
	// Progress runs synchronously on the conversion goroutine. Callers sharing
	// a callback between conversions must synchronize any shared state.
	Progress func(Progress)
}

// Progress measures sampled frames in merged active subtitle intervals, excluding
// blank gaps. Reaching Total means encoding finished, not that File committed.
type Progress struct {
	Processed, Total int64
	DisplaySets      int
}

// Result includes completed display sets and diagnostics, even on failure.
// CLI-wide compatibility warnings are counted separately by the command layer.
type Result struct {
	Frames   int      // Encoded display sets, including clears; not sampled video frames.
	Warnings []string // Repeated identical libass diagnostics are reported only once.
}

// Validate checks canvas bounds, a positive rate, encoding name and font paths.
// It is not a render preflight: font contents and track events are checked later.
// Callers construct Rate and Profile through their respective parsing helpers.
func (o Options) Validate() error {
	if o.Rate.Num <= 0 || o.Rate.Den <= 0 {
		return fmt.Errorf("explicit supported fps is required")
	}
	if _, err := pgs.New(io.Discard, o.Width, o.Height, o.Rate.Code, o.Profile); err != nil {
		return err
	}
	if _, err := subtitle.Decode(nil, o.Encoding); err != nil {
		return err
	}
	for _, dir := range o.Fonts {
		info, err := os.Stat(dir)
		if err != nil {
			return fmt.Errorf("font directory: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("not a font directory: %s", dir)
		}
	}
	return nil
}

// Stream converts one input path into SUP bytes without closing dst.
// Errors or cancellation can leave partial bytes in dst; use File when the
// destination must not become visible until conversion succeeds.
// Cancellation is observed between sampled frames, not inside native calls.
func Stream(ctx context.Context, path string, dst io.Writer, opt Options) (Result, error) {
	var result Result
	if err := opt.Validate(); err != nil {
		return result, err
	}
	enc, err := pgs.New(dst, opt.Width, opt.Height, opt.Rate.Code, opt.Profile)
	if err != nil {
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
	seenWarnings := make(map[string]bool)
	// Drain after loading and each render so repeated animated-frame messages
	// cannot flood results or postpone strict-mode failure until the end.
	check := func() error {
		for _, warning := range r.Diagnostics() {
			if !seenWarnings[warning] {
				result.Warnings = append(result.Warnings, warning)
				seenWarnings[warning] = true
			}
		}
		if opt.Strict && len(result.Warnings) > 0 {
			return fmt.Errorf("strict mode: %s", result.Warnings[0])
		}
		return nil
	}
	intervals, err := r.Load(text)
	if err != nil {
		// Retain diagnostic details, but preserve the original load error.
		_ = check()
		return result, err
	}
	if times != nil {
		if err := r.SetTimes(times); err != nil {
			return result, err
		}
		intervals = times
	}
	if err := check(); err != nil {
		return result, err
	}
	// Spans are half-open frame intervals. Events that land wholly between
	// sampled frames are diagnosed rather than stretched to invent visibility.
	type span struct{ start, end int64 }
	spans := make([]span, 0, len(intervals))
	for i, v := range intervals {
		start, end := opt.Rate.CeilFrame(v.Start), opt.Rate.CeilFrame(v.End)
		if start == end {
			warning := fmt.Sprintf("event %d (%d..%d ms) is invisible at the selected fps", i+1, v.Start, v.End)
			result.Warnings = append(result.Warnings, warning)
			if opt.Strict {
				return result, fmt.Errorf("strict mode: %s", warning)
			}
			continue
		}
		spans = append(spans, span{start, end})
	}
	if len(spans) == 0 {
		return result, fmt.Errorf("no visible subtitle events at the selected fps")
	}
	sort.Slice(spans, func(i, j int) bool { return spans[i].start < spans[j].start })
	// Merge overlaps and touching spans before rendering. libass must see all
	// active layers together, with no synthetic clear between adjacent events.
	merged := []span{spans[0]}
	for _, s := range spans[1:] {
		last := &merged[len(merged)-1]
		if s.start <= last.end {
			last.end = max(last.end, s.end)
		} else {
			merged = append(merged, s)
		}
	}
	var processed, total int64
	for _, s := range merged {
		total += s.end - s.start
	}
	lastReport := time.Now()
	report := func() {
		opt.Progress(Progress{Processed: processed, Total: total, DisplaySets: result.Frames})
		lastReport = time.Now()
	}
	if opt.Progress != nil {
		report()
	}
	var previous *image.NRGBA
	// Compare placement as well as pixels: a moving unchanged bitmap is still
	// a visible update. Keep only the last image, not the full frame sequence.
	write := func(frame int64, im *image.NRGBA) error {
		if previous == nil && im == nil {
			return nil
		}
		if previous != nil && im != nil && previous.Rect == im.Rect && bytes.Equal(previous.Pix, im.Pix) {
			return nil
		}
		if err := enc.Write(opt.Rate.Ticks(frame), im); err != nil {
			return err
		}
		result.Frames++
		previous = im
		return nil
	}
	for _, s := range merged {
		for frame := s.start; frame < s.end; frame++ {
			if err := ctx.Err(); err != nil {
				return result, err
			}
			// libass samples integer milliseconds; round up so an event
			// beginning on this frame is not delayed by a truncation.
			ms := (frame*1000*opt.Rate.Den + opt.Rate.Num - 1) / opt.Rate.Num
			im, changed, err := r.Frame(ms)
			if err != nil {
				return result, err
			}
			if err := check(); err != nil {
				return result, err
			}
			if changed {
				if err := write(frame, im); err != nil {
					return result, err
				}
			}
			processed++
			if opt.Progress != nil && processed < total && time.Since(lastReport) >= 500*time.Millisecond {
				report()
			}
		}
		// Advance libass to the blank boundary as well as writing a clear.
		// Its next change hint must be relative to blank, not the preceding text.
		if _, _, err := r.Frame((s.end*1000*opt.Rate.Den + opt.Rate.Num - 1) / opt.Rate.Num); err != nil {
			return result, err
		}
		if err := check(); err != nil {
			return result, err
		}
		if err := write(s.end, nil); err != nil {
			return result, err
		}
	}
	if result.Frames == 0 {
		return result, fmt.Errorf("libass produced no visible subtitle images")
	}
	if opt.Progress != nil {
		report()
	}
	return result, nil
}

// File stages a conversion beside output, then commits the completed file.
// Existing output survives failures before commit. No-overwrite commits require
// hard-link support; overwrite uses OS rename and is not universally atomic.
// A cleanup error can still be returned after the final output was committed.
func File(ctx context.Context, input, output string, opt Options) (result Result, err error) {
	if !opt.Overwrite {
		if _, err := os.Lstat(output); err == nil {
			return result, fmt.Errorf("output exists (use --overwrite): %s", output)
		} else if !os.IsNotExist(err) {
			return result, err
		}
	}
	// Lexical checks cover case-insensitive destinations; SameFile additionally
	// rejects aliases through symlinks or hard links when both paths exist.
	inAbs, err := filepath.Abs(input)
	if err != nil {
		return result, err
	}
	outAbs, err := filepath.Abs(output)
	if err != nil {
		return result, err
	}
	if strings.EqualFold(inAbs, outAbs) {
		return result, fmt.Errorf("input and output must differ")
	}
	if in, statErr := os.Stat(input); statErr == nil {
		if out, statErr := os.Stat(output); statErr == nil && os.SameFile(in, out) {
			return result, fmt.Errorf("input and output refer to the same file")
		}
	}
	if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
		return result, err
	}
	// Keeping the stage on the destination filesystem avoids cross-device
	// rename/link commits. The temporary filename is never a final deliverable.
	f, err := os.CreateTemp(filepath.Dir(output), ".pgs-converter-*.tmp")
	if err != nil {
		return result, err
	}
	closed := false
	// Named returns let cleanup failures remain visible alongside the original
	// error. After rename the stage is absent; after link it still needs removal.
	defer func() {
		if !closed {
			if closeErr := f.Close(); closeErr != nil {
				if err == nil {
					err = closeErr
				} else {
					err = fmt.Errorf("%w; close temporary output: %v", err, closeErr)
				}
			}
		}
		if removeErr := os.Remove(f.Name()); removeErr != nil && !os.IsNotExist(removeErr) {
			if err == nil {
				err = fmt.Errorf("remove temporary output: %w", removeErr)
			} else {
				err = fmt.Errorf("%w; remove temporary output: %v", err, removeErr)
			}
		}
	}()
	writer := bufio.NewWriterSize(f, 256*1024)
	result, err = Stream(ctx, input, writer, opt)
	if err != nil {
		return result, err
	}
	// Check buffered writes, synchronize file contents and close before commit.
	// This is not a guarantee of directory-entry durability after a system crash.
	if err := writer.Flush(); err != nil {
		return result, err
	}
	if err := f.Sync(); err != nil {
		return result, err
	}
	closed = true
	if err := f.Close(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if opt.Overwrite {
		if err := os.Rename(f.Name(), output); err != nil {
			return result, fmt.Errorf("replace output: %w", err)
		}
	} else {
		// Hard-link creation atomically refuses an existing destination.
		// Unsupported filesystems fail explicitly rather than risking clobbering.
		if err := os.Link(f.Name(), output); err != nil {
			return result, fmt.Errorf("commit output without overwrite: %w", err)
		}
	}
	return result, nil
}
