//go:build !darwin && !windows

package nativepicker

import (
	"context"
	"errors"

	"fyne.io/fyne/v2"
)

func open(_ context.Context, _ fyne.Window, _ Request, complete func([]string, error)) {
	complete(nil, errors.New("native file selection is supported only on macOS and Windows"))
}
