package desktop

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"

	"github.com/ninghaofeng/pgs_converter/internal/nativepicker"
)

func desktopButton(object fyne.CanvasObject, text string) *widget.Button {
	if button, ok := object.(*widget.Button); ok && button.Text == text {
		return button
	}
	var children []fyne.CanvasObject
	switch object := object.(type) {
	case *fyne.Container:
		children = object.Objects
	case *container.Split:
		children = []fyne.CanvasObject{object.Leading, object.Trailing}
	case *container.Scroll:
		children = []fyne.CanvasObject{object.Content}
	}
	for _, child := range children {
		if button := desktopButton(child, text); button != nil {
			return button
		}
	}
	return nil
}

func TestNativePickerEntryPoints(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	w.languagePicker.SetSelected("简体中文")
	for _, tc := range []struct {
		button, title string
		directory     bool
		path          string
	}{
		{"添加文件", "选择字幕文件", false, ""},
		{"添加文件夹", "选择文件夹", true, ""},
		{"浏览", "选择文件夹", true, "/输出 目录"},
		{"添加字体目录", "选择文件夹", true, "/字体 目录"},
	} {
		t.Run(tc.button, func(t *testing.T) {
			called := false
			w.openPicker = func(ctx context.Context, parent fyne.Window, request nativepicker.Request, done func([]string, error)) {
				called = true
				if ctx.Err() != nil || parent != w.win || request.Directory != tc.directory || request.Title != tc.title {
					t.Fatalf("incorrect native picker request: %+v", request)
				}
				if tc.path == "" {
					done(nil, nativepicker.ErrCanceled)
				} else {
					done([]string{tc.path}, nil)
				}
			}
			button := desktopButton(w.win.Content(), tc.button)
			if button == nil {
				t.Fatalf("button %q is missing", tc.button)
			}
			test.Tap(button)
			if !called || w.pickerCancel != nil {
				t.Fatal("entry point did not complete a native picker request")
			}
		})
	}
	if w.output.Text != "/输出 目录" || w.fonts.Text != "/字体 目录" || len(w.rows) != 0 || len(w.logLines) != 0 {
		t.Fatal("native selection changed the wrong fields or cancellation changed the queue")
	}
}

func TestNativePickerResults(t *testing.T) {
	for _, tc := range []struct {
		name      string
		directory bool
		paths     []string
		err       error
		accepted  bool
		reported  bool
	}{
		{"multiple Unicode files", false, []string{"/字幕 one.srt", "/字幕\n二.ass"}, nil, true, false},
		{"directory", true, []string{"/输出 目录"}, nil, true, false},
		{"cancelled", false, nil, nativepicker.ErrCanceled, false, false},
		{"failure", true, nil, errors.New("native diagnostic"), false, true},
		{"empty selection", false, nil, nil, false, true},
		{"empty path", true, []string{""}, nil, false, true},
		{"multiple directories", true, []string{"/one", "/two"}, nil, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := test.NewApp()
			defer a.Quit()
			w := &window{app: a, win: a.NewWindow("test"), selected: -1}
			w.build(Defaults())
			before := w.config()
			w.openPicker = func(_ context.Context, _ fyne.Window, _ nativepicker.Request, done func([]string, error)) {
				done(tc.paths, tc.err)
			}
			accepted := false
			w.selectPaths(tc.directory, func(paths []string) {
				accepted = true
				if !reflect.DeepEqual(paths, tc.paths) {
					t.Fatal("native paths were altered")
				}
			})
			if accepted != tc.accepted || (len(w.logLines) > 0) != tc.reported ||
				!reflect.DeepEqual(before, w.config()) || w.pickerCancel != nil {
				t.Fatal("native result was not accepted, cancelled or reported correctly")
			}
		})
	}
}

func TestNativePickerCancellationAndDebouncing(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	var complete func([]string, error)
	var ctx context.Context
	calls := 0
	w.openPicker = func(context context.Context, _ fyne.Window, _ nativepicker.Request, done func([]string, error)) {
		calls++
		ctx, complete = context, done
	}
	accepted := false
	w.selectPaths(false, func([]string) { accepted = true })
	w.chooseFolder(func(string) { accepted = true })
	if calls != 1 {
		t.Fatal("multiple native panels were opened for the same window")
	}
	w.pickerCancel()
	if ctx.Err() == nil {
		t.Fatal("picker cancellation did not cancel its context")
	}
	complete([]string{"/late-result.srt"}, nil)
	if accepted || len(w.logLines) != 0 || w.pickerCancel != nil {
		t.Fatal("a late native result mutated a closing window")
	}
}

func TestNativeInputPickersRespectBusyState(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	w.active = true
	calls := 0
	w.openPicker = func(_ context.Context, _ fyne.Window, _ nativepicker.Request, done func([]string, error)) {
		calls++
		done(nil, nativepicker.ErrCanceled)
	}
	for _, text := range []string{"Add file", "Add folder"} {
		button := desktopButton(w.win.Content(), text)
		if button == nil {
			t.Fatalf("missing button %q", text)
		}
		test.Tap(button)
	}
	if calls != 0 || !w.active || len(w.logLines) != 2 {
		t.Fatal("input pickers bypassed the active conversion guard")
	}
	w.active = false
	test.Tap(desktopButton(w.win.Content(), "Add file"))
	if calls != 1 {
		t.Fatal("input picker remained blocked after conversion")
	}
}
