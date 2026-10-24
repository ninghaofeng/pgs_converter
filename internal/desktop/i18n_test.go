package desktop

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"image"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ninghaofeng/pgs_converter/internal/batch"
	"github.com/ninghaofeng/pgs_converter/internal/convert"
	"github.com/ninghaofeng/pgs_converter/internal/subtitle"
)

func TestInitialLanguage(t *testing.T) {
	for _, tc := range []struct{ saved, system, want string }{
		{"", "zh-Hans-CN", chinese}, {"", "zh-TW", chinese}, {"", "en-US", english},
		{"", "ja-JP", english}, {english, "zh-CN", english}, {chinese, "en-US", chinese},
	} {
		got, err := initialLanguage(tc.saved, tc.system)
		if err != nil || got != tc.want {
			t.Fatalf("initial language %+v: %s, %v", tc, got, err)
		}
	}
	if got, err := initialLanguage("unsupported", "zh-CN"); got != chinese || err == nil {
		t.Fatalf("invalid saved language was silently accepted: %s, %v", got, err)
	}
}

func TestTranslationCoverageAndFormats(t *testing.T) {
	verbs := regexp.MustCompile(`%[-+# 0-9.]*[a-zA-Z%]`)
	for key, value := range translations {
		if value == "" || !reflect.DeepEqual(verbs.FindAllString(key, -1), verbs.FindAllString(value, -1)) {
			t.Errorf("translation has missing text or mismatched format arguments: %q -> %q", key, value)
		}
	}
	for _, name := range []string{"window.go", "config.go", "scale.go", "picker.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			var name string
			switch fun := call.Fun.(type) {
			case *ast.Ident:
				name = fun.Name
			case *ast.SelectorExpr:
				name = fun.Sel.Name
			}
			index := 0
			switch name {
			case "say":
				index = 1
			case "tr", "label", "button", "check", "formItem", "msg", "uiError":
			default:
				return true
			}
			if len(call.Args) <= index {
				return true
			}
			literal, ok := call.Args[index].(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(literal.Value)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := translations[key]; !ok {
				t.Errorf("missing Chinese translation for %q", key)
			}
			return true
		})
	}
}

func TestLanguageSwitchPreservesRunningState(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: 0, previewGate: make(chan struct{}, 1)}
	w.build(Defaults())
	w.files = []string{"中文路径.ass"}
	w.rows = []row{{job: batch.Job{Input: w.files[0], Output: "output.sup"}, state: msg("Waiting")}}
	w.apply(batch.Event{Index: 0, Progress: &convert.Progress{Processed: 5, Total: 10, DisplaySets: 3}})
	w.active = true
	cancelled := false
	w.cancel = func() { cancelled = true }
	w.start.Disable()
	w.cancelButton.Enable()
	w.say(w.status, "Converting with snapshotted settings...")
	w.say(w.detail, "%s\nOutput: %s", w.files[0], "output.sup")
	w.say(w.previewStatus, "%s at %d ms | SDR style preview; no SUP quantization or HDR luminance simulation.", w.files[0], 501)
	w.timestamp.SetText("501")
	preview := image.NewNRGBA(image.Rect(0, 0, 32, 18))
	w.previewImage.Image = preview
	w.intervals = []subtitle.Interval{{Start: 501, End: 1500}}
	before := w.config()

	w.languagePicker.SetSelected("简体中文")
	if w.start.Text != "开始转换" || w.cancelButton.Text != "取消剩余任务" ||
		w.previewButton.Text != "按时间预览" || !strings.Contains(w.detail.Text, "输出：output.sup") ||
		!strings.Contains(w.previewStatus.Text, "501 毫秒") || w.status.Text != "正在使用本次固定设置转换…" {
		t.Fatal("static or dynamic UI text did not switch to Chinese")
	}
	if !reflect.DeepEqual(before, w.config()) || w.selected != 0 || !w.active || cancelled ||
		!w.start.Disabled() || w.cancelButton.Disabled() || w.rows[0].progress != 0.5 ||
		w.previewImage.Image != preview || w.timestamp.Text != "501" || len(w.intervals) != 1 {
		t.Fatal("changing language mutated the queue, conversion settings, cancellation or preview")
	}
	if a.Preferences().String("desktop-language") != chinese {
		t.Fatal("language selection was not persisted immediately")
	}

	w.apply(batch.Event{Index: 0, Job: batch.Job{Input: w.files[0], Err: context.Canceled}})
	w.finish(true)
	if w.status.Text != "已结束：0 个已保存，0 个失败，1 个取消／未处理。" {
		t.Fatalf("translated task state changed cancellation accounting: %s", w.status.Text)
	}
	w.languagePicker.SetSelected("English")
	if w.start.Text != "Start conversion" ||
		w.status.Text != "Finished: 0 saved, 0 failed, 1 cancelled/not processed." ||
		a.Preferences().String("desktop-language") != english {
		t.Fatal("switching back did not preserve and retranslate terminal state")
	}
}

func TestSavedLanguageAppliedAtStartup(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Preferences().SetString("desktop-language", chinese)
	win := New(a, "test")
	var found bool
	var visit func(fyne.CanvasObject)
	visit = func(object fyne.CanvasObject) {
		if button, ok := object.(*widget.Button); ok && button.Text == "开始转换" {
			found = true
		}
		if container, ok := object.(*fyne.Container); ok {
			for _, child := range container.Objects {
				visit(child)
			}
		}
	}
	visit(win.Content())
	if !found {
		t.Fatal("saved language was not applied to the next window")
	}
}

func TestLocalizedErrorsRetainDiagnostics(t *testing.T) {
	c := Defaults()
	c.Size = "bad"
	_, _, err := c.Options()
	var localized localizedError
	if !errors.As(err, &localized) || !strings.Contains(translate(chinese, localized.key, localized.args...), "分辨率") {
		t.Fatalf("input validation was not localized: %v", err)
	}
	cause := errors.New("original native diagnostic")
	err = uiError("save settings: %v", cause)
	if !errors.Is(err, cause) || err.Error() != "save settings: original native diagnostic" ||
		translate(chinese, "save settings: %v", cause) != "保存设置失败：original native diagnostic" {
		t.Fatal("localization discarded the original diagnostic")
	}
}
