package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2/test"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
)

func TestWindowTitle(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	win := New(a, "dev-build-version")
	defer win.Close()
	if win.Title() != "PGS Converter" {
		t.Fatalf("unexpected application title: %q", win.Title())
	}
}

func TestConfig(t *testing.T) {
	c := Defaults()
	c.Size = "3840x2160"
	c.Profile = "hdr10"
	opt, jobs, err := c.Options()
	if err != nil {
		t.Fatal(err)
	}
	if opt.Width != 3840 || opt.Height != 2160 || opt.Rate.Num != 24000 || opt.Rate.Den != 1001 ||
		!opt.Profile.HDR || opt.Profile.White != 203 || opt.Style.Size != 90 || jobs != 1 {
		t.Fatalf("wrong 4K/HDR snapshot: %+v, jobs %d", opt, jobs)
	}
	for name, mutate := range map[string]func(*Config){
		"oversize":  func(c *Config) { c.Size = "4096x2160" },
		"float-fps": func(c *Config) { c.FPS = "23.976" },
		"jobs":      func(c *Config) { c.Jobs = "33" },
		"font":      func(c *Config) { c.Font = "bad,font" },
		"size":      func(c *Config) { c.FontSize = "0" },
		"margin":    func(c *Config) { c.Margin = "-1" },
		"white":     func(c *Config) { c.White = "NaN" },
		"fonts":     func(c *Config) { c.Fonts = "/must-not-exist-pgs-fonts" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := c
			mutate(&bad)
			if _, _, err := bad.Options(); err == nil {
				t.Fatal("accepted invalid desktop configuration")
			}
		})
	}
}

func TestFinalizationIsNotSuccess(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1, previewGate: make(chan struct{}, 1)}
	w.build(Defaults())
	w.rows = []row{{job: batch.Job{Input: "example.ass"}, state: msg("Waiting")}}
	w.apply(batch.Event{Index: 0, Progress: &convert.Progress{Processed: 10, Total: 10}})
	if w.rows[0].progress >= 1 || w.rows[0].state.key != "Finalizing output..." {
		t.Fatalf("100%% encoding reported committed output: %+v", w.rows[0])
	}
	w.apply(batch.Event{Index: 0, Job: batch.Job{Input: "example.ass", Err: errors.New("commit failed")}})
	w.finish(false)
	if w.rows[0].progress == 1 || w.status.Text != "Finished: 0 saved, 1 failed, 0 cancelled/not processed." {
		t.Fatalf("commit failure was hidden: %s", w.status.Text)
	}
}

func TestCancelledQueueState(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1, previewGate: make(chan struct{}, 1)}
	w.build(Defaults())
	w.rows = []row{{state: msg("Waiting")}, {state: msg("Converting")}}
	w.apply(batch.Event{Index: 1, Job: batch.Job{Input: "example.ass", Err: context.Canceled}})
	w.finish(true)
	if w.rows[0].state.key != "Cancelled before start" || w.rows[1].state.key != "Cancelled" {
		t.Fatalf("cancelled tasks were not distinguished: %+v", w.rows)
	}

}

func TestDesktopConversionEndToEnd(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1, previewGate: make(chan struct{}, 1)}
	c := Defaults()
	c.Size = "1280x720"
	c.Output = t.TempDir()
	w.build(c)
	input, err := filepath.Abs("../../testdata/decoder.ass")
	if err != nil {
		t.Fatal(err)
	}
	w.files = []string{input}
	// Closing is the synchronization point for the final UI callback; polling
	// mutable rows from the test goroutine would race with test-driver fyne.Do.
	closed := make(chan struct{})
	w.win.SetOnClosed(func() { close(closed) })
	w.closing = true
	w.run()
	select {
	case <-closed:
	case <-time.After(15 * time.Second):
		t.Fatal("desktop conversion did not finish and cleanly close")
	}
	if len(w.rows) != 1 || w.rows[0].progress != 1 || !w.rows[0].done {
		t.Fatalf("desktop did not report committed success: %+v", w.rows)
	}
	data, err := os.ReadFile(filepath.Join(c.Output, "decoder.sup"))
	if err != nil || len(data) < 13 || string(data[:2]) != "PG" {
		t.Fatalf("desktop did not produce a SUP: %v", err)
	}
}
