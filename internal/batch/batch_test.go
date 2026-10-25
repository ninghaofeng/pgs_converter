package batch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ninghaofeng/pgs_converter/internal/colorx"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

func TestDiscoveryCollisionsAndDirectories(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "子目录"), 0755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"example.srt", "example.ass", "子目录/动画.ssa"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0644); err != nil {
			t.Fatal(err)
		}
	}
	jobs := Discover([]string{root}, filepath.Join(root, "out"), true)
	if len(jobs) != 3 || jobs[0].Err == nil || jobs[1].Err == nil || jobs[2].Err != nil ||
		jobs[2].Output != filepath.Join(root, "out", "子目录", "动画.sup") {
		t.Fatalf("bad recursive/collision plan: %+v", jobs)
	}
	if jobs := Discover([]string{root}, root, false); len(jobs) != 2 {
		t.Fatalf("nonrecursive discovery descended into folders: %+v", jobs)
	}
}

func TestRunEventsAndCancellation(t *testing.T) {
	rate, err := subtitle.ParseRate("24000/1001")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := colorx.New("sdr", 203)
	if err != nil {
		t.Fatal(err)
	}
	opt := convert.Options{Width: 1280, Height: 720, Rate: rate, Profile: profile, Encoding: "auto"}
	jobs := []Job{{Input: "../../testdata/decoder.ass", Output: filepath.Join(t.TempDir(), "result.sup")}}
	events, err := Run(context.Background(), jobs, opt, 2, true)
	if err != nil {
		t.Fatal(err)
	}
	started, progress, finished := 0, 0, 0
	for event := range events {
		if event.Index != 0 || event.Input != jobs[0].Input {
			t.Fatalf("lost task identity: %+v", event)
		}
		switch {
		case event.Started:
			started++
		case event.Progress != nil:
			progress++
		default:
			finished++
			if event.Err != nil {
				t.Fatal(event.Err)
			}
		}
	}
	if started != 1 || progress < 2 || finished != 1 {
		t.Fatalf("missing events: %d started, %d progress, %d finished", started, progress, finished)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events, err = Run(ctx, jobs, opt, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	for event := range events {
		if event.Started || event.Progress != nil || !errors.Is(event.Err, context.Canceled) {
			t.Fatalf("cancelled queue performed work: %+v", event)
		}
	}
	if _, err := Run(ctx, jobs, opt, 0, true); err == nil {
		t.Fatal("accepted zero workers")
	}
}
