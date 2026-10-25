package desktop

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func visitAboutObjects(root fyne.CanvasObject, visit func(fyne.CanvasObject)) {
	seen := make(map[fyne.CanvasObject]bool)
	var walk func(fyne.CanvasObject)
	walk = func(object fyne.CanvasObject) {
		if object == nil || seen[object] {
			return
		}
		seen[object] = true
		visit(object)
		switch object := object.(type) {
		case *fyne.Container:
			for _, child := range object.Objects {
				walk(child)
			}
		case fyne.Widget:
			for _, child := range test.WidgetRenderer(object).Objects() {
				walk(child)
			}
		}
	}
	walk(root)
}

func TestAboutCopyrightLanguages(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Preferences().SetString("desktop-language", english)
	win := New(a, "1.0.0")
	defer win.Close()
	for _, tc := range []struct {
		language, title, version, author, copyright, close string
	}{
		{"English", "About PGS Converter", "Version: 1.0.0", "Author: ninghaofeng", "Copyright © 2026 ninghaofeng.", "Close"},
		{"简体中文", "关于 PGS Converter", "版本：1.0.0", "作者：ninghaofeng", "版权所有 © 2026 ninghaofeng。", "关闭"},
	} {
		var language *widget.Select
		visitAboutObjects(win.Content(), func(object fyne.CanvasObject) {
			if selectWidget, ok := object.(*widget.Select); ok && len(selectWidget.Options) == 2 && selectWidget.Options[0] == "English" {
				language = selectWidget
			}
		})
		if language == nil {
			t.Fatal("language picker missing")
		}
		language.SetSelected(tc.language)
		var about *widget.Button
		visitAboutObjects(win.Content(), func(object fyne.CanvasObject) {
			if button, ok := object.(*widget.Button); ok && button.Text == tc.title {
				about = button
			}
		})
		if about == nil {
			t.Fatalf("%s: localized About button missing", tc.language)
		}
		test.Tap(about)
		texts := make(map[string]bool)
		var closeButton *widget.Button
		visitAboutObjects(win.Canvas().Overlays().Top(), func(object fyne.CanvasObject) {
			switch object := object.(type) {
			case *widget.Label:
				texts[object.Text] = true
			case *widget.Button:
				if object.Text == tc.close {
					closeButton = object
				}
			}
		})
		for _, expected := range []string{tc.title, "PGS Converter", tc.version, tc.author, tc.copyright} {
			if !texts[expected] {
				t.Errorf("%s: About dialog is missing %q", tc.language, expected)
			}
		}
		if closeButton == nil {
			t.Fatal("localized close button missing")
		}
		test.Tap(closeButton)
		if win.Title() != "PGS Converter" {
			t.Fatal("About information changed the main window title")
		}
	}
}
