package desktop

import (
	"fmt"
	"image"
	"math"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

func TestUIScalePreservesRunningState(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	base := a.Settings().Theme()
	w := &window{app: a, win: a.NewWindow("test"), selected: 0, previewGate: make(chan struct{}, 1)}
	w.build(Defaults())
	if w.scalePicker.Selected != "100%" || a.Settings().Theme() != base {
		t.Fatal("default UI scale changed the existing appearance")
	}
	w.files = []string{"input.ass"}
	w.rows = []row{{job: batch.Job{Input: w.files[0], Output: "output.sup"}, state: msg("Waiting")}}
	w.apply(batch.Event{Index: 0, Progress: &convert.Progress{Processed: 5, Total: 10, DisplaySets: 3}})
	w.active = true
	cancelled := false
	w.cancel = func() { cancelled = true }
	w.start.Disable()
	w.cancelButton.Enable()
	w.say(w.status, "Converting with snapshotted settings...")
	w.timestamp.SetText("501")
	preview := image.NewNRGBA(image.Rect(0, 0, 32, 18))
	w.previewImage.Image = preview
	w.intervals = []subtitle.Interval{{Start: 501, End: 1500}}
	before := w.config()
	rows := append([]row(nil), w.rows...)

	for _, percent := range []int{80, 150, 75, 90, 110, 125, 100, 80} {
		w.scalePicker.SetSelected(fmt.Sprintf("%d%%", percent))
		for _, name := range []fyne.ThemeSizeName{theme.SizeNameText, theme.SizeNameHeadingText,
			theme.SizeNameCaptionText, theme.SizeNameInlineIcon, theme.SizeNamePadding, theme.SizeNameInnerPadding} {
			want := base.Size(name) * float32(percent) / 100
			got := a.Settings().Theme().Size(name)
			if math.Abs(float64(got-want)) > 0.0001 {
				t.Fatalf("%d%% size %s: got %f, want %f", percent, name, got, want)
			}
		}
		if a.Preferences().Int("desktop-ui-scale") != percent {
			t.Fatal("UI scale was not saved immediately")
		}
		if percent == 100 && a.Settings().Theme() != base {
			t.Fatal("100% did not restore the original theme")
		}
		cell := w.timestampBox.MinSize()
		if cell.Height < w.timestamp.MinSize().Height || cell.Width != 140*float32(percent)/100 {
			t.Fatalf("timestamp field clips at %d%%: %v vs minimum %v", percent, cell, w.timestamp.MinSize())
		}
		if !reflect.DeepEqual(before, w.config()) || !reflect.DeepEqual(rows, w.rows) ||
			w.selected != 0 || !w.active || cancelled || !w.start.Disabled() || w.cancelButton.Disabled() ||
			w.previewImage.Image != preview || w.timestamp.Text != "501" || len(w.intervals) != 1 ||
			w.status.Text != "Converting with snapshotted settings..." {
			t.Fatal("changing UI scale mutated conversion, queue or preview state")
		}
	}
	w.languagePicker.SetSelected("简体中文")
	if w.scalePicker.Selected != "80%" || w.uiScale != 80 {
		t.Fatal("changing language reset UI scale")
	}
	w.scalePicker.SetSelected("90%")
	if w.language != chinese || w.start.Text != "开始转换" {
		t.Fatal("changing scale reset the language")
	}
}

func TestSavedUIScaleAppliedAtStartup(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	baseSize := a.Settings().Theme().Size(theme.SizeNameText)
	a.Preferences().SetInt("desktop-ui-scale", 80)
	for range 2 {
		win := New(a, "test")
		if got := a.Settings().Theme().Size(theme.SizeNameText); math.Abs(float64(got-baseSize*0.8)) > 0.0001 {
			t.Fatalf("saved scale was missing or compounded at startup: %f", got)
		}
		win.Close()
	}
}

func TestInvalidUIScaleReported(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Preferences().SetInt("desktop-ui-scale", 999)
	win := New(a, "test")
	defer win.Close()
	if _, ok := a.Settings().Theme().(*scaledTheme); ok {
		t.Fatal("unsupported saved scale was applied")
	}
	if win.Canvas().Overlays().Top() == nil {
		t.Fatal("invalid saved scale was not reported to the user")
	}
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	before := a.Settings().Theme()
	w.scalePicker.OnChanged("invalid")
	if a.Settings().Theme() != before || w.uiScale != 100 ||
		a.Preferences().Int("desktop-ui-scale") != 999 ||
		!strings.Contains(w.log.Text, "Unsupported UI scale") {
		t.Fatal("invalid selection silently changed the scale or preferences")
	}
}
