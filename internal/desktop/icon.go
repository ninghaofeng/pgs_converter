package desktop

import (
	_ "embed"

	"fyne.io/fyne/v2"
)

//go:embed assets/icon.png
var iconPNG []byte

var applicationIcon = fyne.NewStaticResource("pgs-converter.png", iconPNG)
