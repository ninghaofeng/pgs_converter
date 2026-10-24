package nativepicker

import (
	"context"
	"errors"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
	"github.com/ncruces/zenity"
)

func open(ctx context.Context, parent fyne.Window, request Request, complete func([]string, error)) {
	native, ok := parent.(driver.NativeWindow)
	if !ok {
		complete(nil, errors.New("native Windows window is unavailable"))
		return
	}
	native.RunNative(func(value any) {
		window, ok := value.(driver.WindowsWindowContext)
		if !ok || window.HWND == 0 {
			complete(nil, errors.New("native Windows window handle is unavailable"))
			return
		}
		go func() {
			options := []zenity.Option{zenity.Title(request.Title), zenity.Attach(window.HWND), zenity.Context(ctx)}
			var paths []string
			var err error
			if request.Directory {
				var path string
				path, err = zenity.SelectFile(append(options, zenity.Directory())...)
				if err == nil {
					paths = []string{path}
				}
			} else {
				options = append(options, zenity.FileFilter{Name: "SRT / ASS / SSA", Patterns: []string{"*.srt", "*.ass", "*.ssa"}})
				paths, err = zenity.SelectFileMultiple(options...)
			}
			if errors.Is(err, zenity.ErrCanceled) {
				err = ErrCanceled
			}
			complete(paths, err)
		}()
	})
}
