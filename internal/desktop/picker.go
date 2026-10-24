package desktop

import (
	"context"
	"errors"

	"fyne.io/fyne/v2"

	"github.com/ninghaofeng/pgs_converter/internal/nativepicker"
)

func (w *window) selectPaths(directory bool, accept func([]string)) {
	if w.pickerCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.pickerCancel = cancel
	title := w.tr("Choose subtitle files")
	if directory {
		title = w.tr("Choose folder")
	}
	open := w.openPicker
	if open == nil {
		open = nativepicker.Open
	}
	open(ctx, w.win, nativepicker.Request{Title: title, Directory: directory}, func(paths []string, err error) {
		fyne.Do(func() {
			w.pickerCancel = nil
			cancelled := ctx.Err() != nil
			cancel()
			if cancelled || errors.Is(err, nativepicker.ErrCanceled) {
				return
			}
			if err != nil {
				w.showError(uiError("Could not open the system file selection panel: %v", err))
				return
			}
			valid := len(paths) > 0 && (!directory || len(paths) == 1)
			for _, path := range paths {
				valid = valid && path != ""
			}
			if !valid {
				w.showError(uiError("The system file selection panel returned an invalid selection."))
				return
			}
			accept(paths)
		})
	})
}
