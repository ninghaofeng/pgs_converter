// Package nativepicker opens operating-system file and directory selection panels.
package nativepicker

import (
	"context"
	"errors"

	"fyne.io/fyne/v2"
)

// ErrCanceled distinguishes a user's dismissal from an OS dialog failure.
var ErrCanceled = errors.New("native file selection cancelled")

// Request selects either multiple subtitle files or exactly one directory.
// Title is application-localized; native controls follow the operating system.
type Request struct {
	Title     string
	Directory bool
}

// Open starts selection without waiting for user interaction. Validation or an
// already-cancelled context can invoke complete inline; otherwise it may run off
// the UI thread. Callers must marshal the single completion before touching UI.
func Open(ctx context.Context, parent fyne.Window, request Request, complete func([]string, error)) {
	if err := ctx.Err(); err != nil {
		complete(nil, err)
		return
	}
	open(ctx, parent, request, complete)
}
