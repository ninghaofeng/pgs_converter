package desktop

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
)

func (w *window) showAbout() {
	icon := canvas.NewImageFromResource(applicationIcon)
	icon.SetMinSize(fyne.NewSize(96, 96))
	icon.FillMode = canvas.ImageFillContain
	notices := widget.NewLabel(w.tr("Third-party components retain their own copyrights and licenses. See THIRD_PARTY_NOTICES and the licenses folder in the application package."))
	notices.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(
		container.NewCenter(icon),
		widget.NewLabelWithStyle("PGS Converter", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		widget.NewLabel(w.tr("Version: %s", w.version)),
		widget.NewLabel(w.tr("Author: %s", "ninghaofeng")),
		widget.NewLabel(w.tr("Copyright © 2026 ninghaofeng.")),
		notices,
	)
	d := dialog.NewCustom(w.tr("About PGS Converter"), w.tr("Close"), content, w.win)
	d.Resize(fyne.NewSize(480, 380))
	d.Show()
}
