package desktop

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

// Keep the runtime icon independent of the working directory and bundle layout.
// The SVG generator also supplies the matching ICNS and Windows COFF resources.
//
//go:embed assets/icon.png
var iconPNG []byte

var applicationIcon = fyne.NewStaticResource("pgs-converter.png", iconPNG)
