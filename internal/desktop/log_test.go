package desktop

import (
	"image/color"
	"math"
	"strings"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
)

func contrastRatio(a, b color.Color) float64 {
	luminance := func(c color.Color) float64 {
		r, g, b, _ := c.RGBA()
		linear := func(value uint32) float64 {
			v := float64(value) / 65535
			if v <= 0.04045 {
				return v / 12.92
			}
			return math.Pow((v+0.055)/1.055, 2.4)
		}
		return 0.2126*linear(r) + 0.7152*linear(g) + 0.0722*linear(b)
	}
	x, y := luminance(a), luminance(b)
	return (max(x, y) + 0.05) / (min(x, y) + 0.05)
}

func TestLogContrastAndReadOnly(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	w := &window{app: a, win: a.NewWindow("test"), selected: -1}
	w.build(Defaults())
	w.appendLog("ERROR: readable diagnostic")
	for _, tc := range []struct {
		name    string
		base    fyne.Theme
		variant fyne.ThemeVariant
	}{
		{"light", theme.LightTheme(), theme.VariantLight},
		{"dark", theme.DarkTheme(), theme.VariantDark},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a.Settings().SetTheme(tc.base)
			for _, scale := range []string{"100%", "80%", "150%", "100%"} {
				w.scalePicker.SetSelected(scale)
				w.log.Refresh()
				th := w.log.Theme()
				foreground := th.Color(theme.ColorNameDisabled, tc.variant)
				background := th.Color(theme.ColorNameInputBackground, tc.variant)
				if ratio := contrastRatio(foreground, background); ratio < 4.5 {
					t.Fatalf("%s %s log contrast %.2f is below 4.5:1", tc.name, scale, ratio)
				}
				if th.Size(theme.SizeNameText) != a.Settings().Theme().Size(theme.SizeNameText) {
					t.Fatal("log theme stopped following UI scale")
				}
				found := false
				visited := make(map[fyne.CanvasObject]bool)
				var visit func(fyne.CanvasObject)
				visit = func(object fyne.CanvasObject) {
					if visited[object] {
						return
					}
					visited[object] = true
					switch object := object.(type) {
					case *canvas.Text:
						if strings.Contains(object.Text, "readable diagnostic") {
							found = true
							if ratio := contrastRatio(object.Color, background); ratio < 4.5 {
								t.Fatalf("rendered log text contrast %.2f is below 4.5:1", ratio)
							}
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
				visit(w.log)
				if !found {
					t.Fatal("rendered log diagnostic was not found")
				}
				if !w.log.Disabled() || w.font.Disabled() {
					t.Fatal("log read-only state or other input state changed")
				}
			}
		})
	}
	copy := desktopButton(w.win.Content(), "Copy log")
	if copy == nil {
		t.Fatal("copy log button is missing")
	}
	test.Tap(copy)
	if a.Clipboard().Content() != "ERROR: readable diagnostic" {
		t.Fatal("copy log no longer preserves original diagnostics")
	}
}
