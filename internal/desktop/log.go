package desktop

import (
	"image/color"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/theme"
)

// logTheme is scoped to the disabled, read-only log entry. Resolve the current
// application theme on every call so live color and UI-scale changes still apply.
type logTheme struct{ settings fyne.Settings }

func (t logTheme) Color(name fyne.ThemeColorName, variant fyne.ThemeVariant) color.Color {
	// Logs are read-only, but must not use low-contrast disabled text.
	if name == theme.ColorNameDisabled {
		name = theme.ColorNameForeground
	}
	return t.settings.Theme().Color(name, variant)
}

func (t logTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.settings.Theme().Font(style)
}

func (t logTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.settings.Theme().Icon(name)
}

func (t logTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.settings.Theme().Size(name)
}
