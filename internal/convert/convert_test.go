package convert

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
	"github.com/ninghaofeng/pgs_converter/internal/render"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

func options() Options {
	rate, _ := subtitle.ParseRate("24")
	profile, _ := colorx.New("sdr", 203)
	return Options{Width: 640, Height: 360, Rate: rate, Profile: profile, Style: subtitle.Style{Font: "sans-serif", Size: 28, Margin: 20}}
}

func compositions(t *testing.T, data []byte) []struct {
	pts   uint32
	count byte
} {
	t.Helper()
	var out []struct {
		pts   uint32
		count byte
	}
	for len(data) > 0 {
		if len(data) < 13 {
			t.Fatal("truncated SUP")
		}
		n := int(binary.BigEndian.Uint16(data[11:13]))
		if len(data) < 13+n {
			t.Fatal("truncated payload")
		}
		if data[10] == 0x16 {
			out = append(out, struct {
				pts   uint32
				count byte
			}{binary.BigEndian.Uint32(data[2:6]), data[23]})
		}
		data = data[13+n:]
	}
	return out
}

type delayedWriter struct {
	bytes.Buffer
	delayed bool
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func (w *delayedWriter) Write(p []byte) (int, error) {
	if !w.delayed {
		// Cross the 500 ms progress throttle without requiring a large fixture.
		time.Sleep(550 * time.Millisecond)
		w.delayed = true
	}
	return w.Buffer.Write(p)
}

func TestProgress(t *testing.T) {
	const fixture = "../../testdata/decoder.ass"
	opt := options()
	var updates []Progress
	opt.Progress = func(p Progress) { updates = append(updates, p) }
	var output delayedWriter
	result, err := Stream(context.Background(), fixture, &output, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) < 3 || updates[0].Processed != 0 {
		t.Fatalf("missing initial or live progress: %+v", updates)
	}
	for i, p := range updates {
		// The two active intervals cover 60 frames; overlaps and blank gaps
		// must not inflate the denominator.
		if p.Total != 60 || p.Processed < 0 || p.Processed > p.Total {
			t.Fatalf("invalid progress: %+v", p)
		}
		if i > 0 && p.Processed <= updates[i-1].Processed {
			t.Fatalf("non-increasing progress: %+v", updates)
		}
	}
	final := updates[len(updates)-1]
	if final.Processed != final.Total || final.DisplaySets != result.Frames {
		t.Fatalf("incomplete final progress: %+v", final)
	}
	opt.Progress = nil
	var baseline bytes.Buffer
	if _, err := Stream(context.Background(), fixture, &baseline, opt); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(output.Bytes(), baseline.Bytes()) {
		t.Fatal("progress changed the encoded output")
	}

	updates = nil
	opt.Progress = func(p Progress) { updates = append(updates, p) }
	if _, err := Stream(context.Background(), fixture, failingWriter{}, opt); err != io.ErrClosedPipe {
		t.Fatalf("expected write failure, got %v", err)
	}
	if len(updates) != 1 || updates[0].Processed != 0 {
		t.Fatalf("write failure reported completed progress: %+v", updates)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	updates = nil
	opt.Progress = func(p Progress) {
		updates = append(updates, p)
		cancel()
	}
	if _, err := Stream(ctx, fixture, io.Discard, opt); err != context.Canceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
	if len(updates) != 1 || updates[0].Processed != 0 {
		t.Fatalf("cancellation reported completed progress: %+v", updates)
	}
}

func TestStreamTimingAndClear(t *testing.T) {
	var buf bytes.Buffer
	result, err := Stream(context.Background(), "../../testdata/sample.srt", &buf, options())
	if err != nil {
		t.Fatal(err)
	}
	sets := compositions(t, buf.Bytes())
	if len(sets) != 4 || result.Frames != 4 {
		t.Fatalf("expected four static updates, got %d: %+v", result.Frames, sets)
	}
	want := []uint32{48750, 90000, 138750, 180000}
	for i, v := range sets {
		if v.pts != want[i] {
			t.Errorf("set %d: %d want %d", i, v.pts, want[i])
		}
	}
	if sets[len(sets)-1].count != 0 {
		t.Fatal("missing final clear")
	}
}

func TestSafeOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sup")
	original := []byte("existing")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := File(context.Background(), "../../testdata/sample.srt", path, options()); err == nil {
		t.Fatal("overwrote without permission")
	}
	opt := options()
	opt.Overwrite = true
	if _, err := File(context.Background(), filepath.Join(dir, "missing.srt"), path, opt); err == nil {
		t.Fatal("missing input succeeded")
	}
	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, original) {
		t.Fatal("failed conversion changed existing output")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := File(ctx, "../../testdata/sample.srt", path, opt); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := File(context.Background(), "../../testdata/sample.srt", path, opt); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("left temporary files: %v %v", entries, err)
	}
}

func TestMissingGlyphDiagnostics(t *testing.T) {
	input := filepath.Join(t.TempDir(), "missing-glyph.srt")
	// A Unicode noncharacter avoids depending on which ordinary glyphs the
	// test machine's installed fallback fonts happen to cover.
	if err := os.WriteFile(input, []byte("1\n00:00:00,000 --> 00:00:01,000\n\U0010ffff"), 0600); err != nil {
		t.Fatal(err)
	}
	opt := options()
	opt.Strict = true
	result, err := Stream(context.Background(), input, &bytes.Buffer{}, opt)
	if err == nil || len(result.Warnings) == 0 {
		t.Fatalf("missing glyph was not diagnosed in strict mode: %v %v", result, err)
	}
}

func TestInvisibleStrictAndHDR(t *testing.T) {
	input := filepath.Join(t.TempDir(), "short.srt")
	if err := os.WriteFile(input, []byte("1\n00:00:00,001 --> 00:00:00,002\nx"), 0600); err != nil {
		t.Fatal(err)
	}
	opt := options()
	opt.Strict = true
	if _, err := Stream(context.Background(), input, &bytes.Buffer{}, opt); err == nil {
		t.Fatal("strict mode accepted invisible event")
	}
	opt = options()
	opt.Profile, _ = colorx.New("hdr10", 203)
	var first, second bytes.Buffer
	if _, err := Stream(context.Background(), "../../testdata/sample.srt", &first, opt); err != nil {
		t.Fatal(err)
	}
	if _, err := Stream(context.Background(), "../../testdata/sample.srt", &second, opt); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("HDR output is nondeterministic")
	}
	var sdr bytes.Buffer
	if _, err := Stream(context.Background(), "../../testdata/sample.srt", &sdr, options()); err != nil {
		t.Fatal(err)
	}
	hdrData, sdrData := first.Bytes(), sdr.Bytes()
	whiteFound := false
	for len(hdrData) > 0 {
		n := int(binary.BigEndian.Uint16(hdrData[11:13]))
		if n != int(binary.BigEndian.Uint16(sdrData[11:13])) {
			t.Fatal("profile changed segment dimensions")
		}
		a, b := hdrData[13:13+n], sdrData[13:13+n]
		if hdrData[10] == 0x14 {
			for i := 2; i < n; i += 5 {
				if a[i] != b[i] || a[i+4] != b[i+4] {
					t.Fatal("HDR changed palette indices or transparency")
				}
				if b[i+1] == 235 && b[i+2] == 128 && b[i+3] == 128 && b[i+4] == 255 {
					whiteFound = true
					if a[i+1] != 143 || a[i+2] != 128 || a[i+3] != 128 {
						t.Fatalf("HDR reference white is not 203 nits: %v", a[i:i+5])
					}
				}
			}
		} else if !bytes.Equal(a, b) {
			t.Fatal("color profile changed geometry, RLE or timing")
		}
		hdrData, sdrData = hdrData[13+n:], sdrData[13+n:]
	}
	if !whiteFound {
		t.Fatal("reference fixture has no opaque white")
	}
}

func TestAnimatedTextStream(t *testing.T) {
	opt := options()
	opt.Width, opt.Height = 1280, 720
	var first, second bytes.Buffer
	result, err := Stream(context.Background(), "../../testdata/animated.ass", &first, opt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Stream(context.Background(), "../../testdata/animated.ass", &second, opt); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first.Bytes(), second.Bytes()) {
		t.Fatal("animated text output is nondeterministic")
	}
	sets := compositions(t, first.Bytes())
	if result.Frames < 30 || len(sets) != result.Frames {
		t.Fatalf("text animation was lost: %d updates, %d compositions", result.Frames, len(sets))
	}
	for i, set := range sets {
		if i > 0 && set.pts <= sets[i-1].pts {
			t.Fatal("text animation timestamps are not increasing")
		}
	}
	for _, pts := range []uint32{225000, 315000} {
		found := false
		for _, set := range sets {
			if set.pts == pts && set.count == 0 {
				found = true
			}
		}
		if !found {
			t.Fatalf("text animation did not clear at %d", pts)
		}
	}
}

func TestFFmpegDecode(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("independent decoder unavailable: install FFmpeg to run interoperability test")
	}
	// System-font glyph coverage varies by platform and can trigger lossy
	// palette reduction. Keep the decoder's RGB limits independent of that
	// quantization by using font-free drawings with at most 255 visible colors.
	const fixture = "../../testdata/decoder.ass"
	output := filepath.Join(t.TempDir(), "decoder.sup")
	opt := options()
	// FFmpeg guesses BT.601 for <=576-line subtitles. Use HD here so its
	// independent decoder interprets this encoder's BT.709 palette as intended.
	opt.Width, opt.Height = 1280, 720
	result, err := File(context.Background(), fixture, output, opt)
	if err != nil {
		t.Fatal(err)
	}
	if result.Frames < 30 {
		t.Fatalf("animation was lost: %d updates", result.Frames)
	}
	mkv := filepath.Join(t.TempDir(), "subtitles.mkv")
	// Copy timestamps: normalizing the subtitle's first PTS to zero would hide
	// timing errors and shift it relative to the synthetic background video.
	mux := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-copyts", "-i", output, "-map", "0:s:0", "-c:s", "copy", mkv)
	if log, err := mux.CombinedOutput(); err != nil {
		t.Fatalf("independent MKV mux: %v: %s", err, log)
	}
	demux := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-copyts", "-i", mkv, "-map", "0:s:0", "-c:s", "copy", "-f", "sup", "pipe:1")
	if data, err := demux.Output(); err != nil || len(compositions(t, data)) != result.Frames {
		t.Fatalf("MKV roundtrip lost compositions: %v", err)
	}
	// Select representative fade/movement, overlap, gap and drawing frames.
	// RGB overlay avoids adding chroma subsampling error to the palette check.
	cmd := exec.Command(ffmpeg, "-hide_banner", "-loglevel", "error", "-copyts", "-f", "lavfi", "-i", "color=black:s=1280x720:r=24:d=4,format=rgba",
		"-i", output, "-filter_complex", "[0:v][1:s]overlay=format=rgb,select='eq(n,0)+eq(n,14)+eq(n,18)+eq(n,30)+eq(n,48)+eq(n,64)+eq(n,77)+eq(n,90)'",
		"-t", "4", "-fps_mode", "passthrough", "-pix_fmt", "rgba", "-f", "rawvideo", "pipe:1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("independent decode: %v: %s", err, stderr.String())
	}
	frameSize := 1280 * 720 * 4
	if len(raw) != 8*frameSize {
		t.Fatalf("decoded %d bytes, expected %d", len(raw), 8*frameSize)
	}
	script, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	r, err := render.New(1280, 720, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if _, err := r.Load(string(script)); err != nil {
		t.Fatal(err)
	}
	for index, frame := range []int{0, 14, 18, 30, 48, 64, 77, 90} {
		t.Run(strconv.Itoa(frame), func(t *testing.T) {
			im, _, err := r.Frame(int64((frame*1000 + 23) / 24))
			if err != nil {
				t.Fatal(err)
			}
			colors := make(map[color.NRGBA]struct{})
			if im != nil {
				for y := im.Rect.Min.Y; y < im.Rect.Max.Y; y++ {
					for x := im.Rect.Min.X; x < im.Rect.Max.X; x++ {
						c := im.NRGBAAt(x, y)
						if c.A != 0 {
							colors[c] = struct{}{}
						}
					}
				}
			}
			if len(colors) > 255 {
				t.Fatalf("decoder fixture requires lossy quantization: %d visible colors", len(colors))
			}
			expected := image.NewRGBA(image.Rect(0, 0, 1280, 720))
			draw.Draw(expected, expected.Rect, image.NewUniform(color.Black), image.Point{}, draw.Src)
			if im != nil {
				draw.Draw(expected, im.Rect, im, im.Rect.Min, draw.Over)
			}
			decoded := raw[index*frameSize : (index+1)*frameSize]
			total, maxError, active := 0, 0, 0
			for i := 0; i < frameSize; i += 4 {
				for c := 0; c < 3; c++ {
					d := int(decoded[i+c]) - int(expected.Pix[i+c])
					if d < 0 {
						d = -d
					}
					total += d
					maxError = max(maxError, d)
				}
				if expected.Pix[i] != 0 || expected.Pix[i+1] != 0 || expected.Pix[i+2] != 0 {
					active++
				}
			}
			// Normalize against active source pixels, not the large black canvas,
			// which would otherwise hide missing, displaced or incorrectly colored text.
			// Blank frames require exact black to catch failed clearing.
			t.Logf("active=%d mean-error=%g peak-error=%d", active, float64(total)/float64(max(active*3, 1)), maxError)
			if active == 0 {
				if maxError != 0 {
					t.Fatalf("subtitle residue on blank frame: peak=%d", maxError)
				}
			} else if mean := float64(total) / float64(active*3); mean > 2 || maxError > 16 {
				t.Fatalf("decode/source mismatch: mean=%g peak=%d", mean, maxError)
			}
		})
	}
}
