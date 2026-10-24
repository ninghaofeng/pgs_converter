// Package nativepicker opens operating-system file and directory selection panels.
package nativepicker

import (
	"context"
	"errors"

	"fyne.io/fyne/v2"
)

var ErrCanceled = errors.New("native file selection cancelled")

type Request struct {
	Title     string
	Directory bool
}

// Open returns immediately. The callback runs once and may run off the UI thread.
func Open(ctx context.Context, parent fyne.Window, request Request, complete func([]string, error)) {
	if err := ctx.Err(); err != nil {
		complete(nil, err)
		return
	}
	open(ctx, parent, request, complete)
}
