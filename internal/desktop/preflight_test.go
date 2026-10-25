package desktop

import (
	"context"
	"testing"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
)

func preflightButton(w fyne.Window, text string) *widget.Button {
	seen := make(map[fyne.CanvasObject]bool)
	var found *widget.Button
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		if object == nil || seen[object] {
			return
		}
		seen[object] = true
		switch object := object.(type) {
		case *widget.Button:
			if object.Text == text {
				found = object
			}
		case *fyne.Container:
			for _, child := range object.Objects {
				visit(child)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(object).Objects() {
				visit(child)
			}
		}
	}
	visit(w.Canvas().Overlays().Top())
	return found
}

func TestMissingFontsRequireExplicitGUIChoice(t *testing.T) {
	for _, continueConversion := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "continue"}[continueConversion], func(t *testing.T) {
			a := test.NewApp()
			defer a.Quit()
			w := &window{app: a, win: a.NewWindow("test"), selected: -1}
			w.build(Defaults())
			w.languagePicker.SetSelected("简体中文")
			w.rows = []row{{job: batch.Job{Input: "missing.ass"}, state: msg("Waiting")}}
			w.active = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			continued := false
			w.confirmMissingFonts(ctx, cancel, []missingFonts{{"missing.ass", []string{"Missing Font"}}}, false, func() {
				continued = true
				cancel()
			})
			if continued || !w.active {
				t.Fatal("conversion continued or was cancelled before the user's decision")
			}
			text := "取消转换"
			if continueConversion {
				text = "允许字体回退并继续"
			}
			button := preflightButton(w.win, text)
			if button == nil {
				t.Fatalf("missing localized decision button %q", text)
			}
			test.Tap(button)
			if continued != continueConversion {
				t.Fatal("missing-font confirmation ignored the user's choice")
			}
			if continueConversion && !w.active {
				t.Fatal("confirmation completion unexpectedly finalized the conversion")
			}
			if !continueConversion && (w.active || w.rows[0].done || w.rows[0].state.key != "Cancelled before start") {
				t.Fatal("cancelling preflight did not leave the task unprocessed")
			}
		})
	}
}

func TestMissingFontsStrictGUIBlocks(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	w.rows = []row{{job: batch.Job{Input: "missing.ssa"}, state: msg("Waiting")}}
	w.active = true
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w.confirmMissingFonts(ctx, cancel, []missingFonts{{"missing.ssa", []string{"Missing Font"}}}, true, func() {
		t.Fatal("strict GUI preflight allowed font fallback")
	})
	if w.active || !w.rows[0].done || w.rows[0].state.key != "Failed: %s" || ctx.Err() == nil {
		t.Fatal("strict GUI did not mark missing-font input as failed")
	}
	if preflightButton(w.win, "Continue with font fallback") != nil {
		t.Fatal("strict mode offered a forbidden fallback")
	}
}

func TestMissingFontPromptCancelsWithWindow(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	w.rows = []row{{job: batch.Job{Input: "missing.ass"}, state: msg("Waiting")}}
	w.active, w.closing = true, true
	closed := make(chan struct{})
	w.win.SetOnClosed(func() { close(closed) })
	ctx, cancel := context.WithCancel(context.Background())
	w.confirmMissingFonts(ctx, cancel, []missingFonts{{"missing.ass", []string{"Missing Font"}}}, false, func() {
		t.Error("cancelled prompt continued conversion")
	})
	cancel()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled font prompt did not finish and close the window")
	}
	if w.active {
		t.Fatal("preflight remained active after cancellation")
	}
}
