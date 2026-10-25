package desktop

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
)

var uiScalePercentages = []int{75, 80, 90, 100, 110, 125, 150}

// scaledTheme changes logical toolkit sizes only. OS/Retina scaling and subtitle
// pixel dimensions remain independent of the user's interface-size preference.
type scaledTheme struct {
	fyne.Theme
	factor float32
}

func (t *scaledTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.Theme.Size(name) * t.factor
}

func (w *window) applyUIScale(percent int) {
	base := w.app.Settings().Theme()
	previous, scaled := base.(*scaledTheme)
	if scaled {
		// Always scale the underlying theme; wrapping again would compound
		// every scale change instead of applying the selected percentage.
		base = previous.Theme
	}
	if percent == 100 {
		if scaled {
			w.app.Settings().SetTheme(base)
		}
	} else {
		if base == nil {
			base = theme.DefaultTheme()
		}
		w.app.Settings().SetTheme(&scaledTheme{Theme: base, factor: float32(percent) / 100})
	}
	w.uiScale = percent
	if w.timestampBox != nil {
		w.timestampBox.Layout = layout.NewGridWrapLayout(w.timestampSize())
		w.timestampBox.Refresh()
	}
}

func (w *window) timestampSize() fyne.Size {
	return fyne.NewSize(140*float32(w.uiScale)/100, w.timestamp.MinSize().Height)
}

func (w *window) buildScalePicker() {
	if w.uiScale == 0 {
		w.uiScale = 100
	}
	w.applyUIScale(w.uiScale)
	options := make([]string, len(uiScalePercentages))
	for i, percent := range uiScalePercentages {
		options[i] = fmt.Sprintf("%d%%", percent)
	}
	w.scalePicker = w.selector(options, fmt.Sprintf("%d%%", w.uiScale), nil)
	w.scalePicker.OnChanged = func(value string) {
		percent, err := strconv.Atoi(strings.TrimSuffix(value, "%"))
		if err != nil || !slices.Contains(uiScalePercentages, percent) {
			w.showError(uiError("Unsupported UI scale: %s", value))
			return
		}
		w.applyUIScale(percent)
		w.app.Preferences().SetInt("desktop-ui-scale", percent)
	}
}
