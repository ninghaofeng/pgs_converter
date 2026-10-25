package convert

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestPreviewSRTMillisecondBoundaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "精确时间.srt")
	if err := os.WriteFile(path, []byte("1\n00:00:00,503 --> 00:00:01,507\nPreview\n"), 0644); err != nil {
		t.Fatal(err)
	}
	opt := options()
	opt.Encoding = "auto"
	for _, ms := range []int64{502, 503, 1506, 1507} {
		result, err := Preview(context.Background(), path, opt, ms)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Intervals) != 1 || result.Intervals[0].Start != 503 || result.Intervals[0].End != 1507 {
			t.Fatalf("lost exact SRT times: %+v", result.Intervals)
		}
		if result.Image.Bounds().Dx() != opt.Width || result.Image.Bounds().Dy() != opt.Height {
			t.Fatal("preview did not preserve target canvas")
		}
		visible := false
		for i := 3; i < len(result.Image.Pix); i += 4 {
			visible = visible || result.Image.Pix[i] != 0
		}
		if visible != (ms >= 503 && ms < 1507) {
			t.Fatalf("wrong visibility at %d ms: %v", ms, visible)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Preview(ctx, path, opt, 503); !errors.Is(err, context.Canceled) {
		t.Fatalf("preview ignored cancellation: %v", err)
	}
	for _, ms := range []int64{-1, 47721859} {
		if _, err := Preview(context.Background(), path, opt, ms); err == nil {
			t.Fatalf("accepted invalid timestamp %d", ms)
		}
	}
}
