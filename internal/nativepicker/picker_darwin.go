package nativepicker

/*
#cgo LDFLAGS: -framework AppKit -framework UniformTypeIdentifiers
#include <stdint.h>
#include <stdlib.h>
void pgs_picker_open(uintptr_t parent, const char *title, int directory, uintptr_t handle);
void pgs_picker_cancel(uintptr_t handle);
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/cgo"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver"
)

type pendingPicker struct {
	ctx      context.Context
	complete func([]string, error)
	done     chan struct{}
}

func open(ctx context.Context, parent fyne.Window, request Request, complete func([]string, error)) {
	native, ok := parent.(driver.NativeWindow)
	if !ok {
		complete(nil, errors.New("native macOS window is unavailable"))
		return
	}
	native.RunNative(func(value any) {
		window, ok := value.(driver.MacWindowContext)
		if !ok || window.NSWindow == 0 {
			complete(nil, errors.New("native macOS window handle is unavailable"))
			return
		}
		pending := &pendingPicker{ctx: ctx, complete: complete, done: make(chan struct{})}
		handle := cgo.NewHandle(pending)
		title := C.CString(request.Title)
		defer C.free(unsafe.Pointer(title))
		directory := C.int(0)
		if request.Directory {
			directory = 1
		}
		C.pgs_picker_open(C.uintptr_t(window.NSWindow), title, directory, C.uintptr_t(handle))
		go func() {
			select {
			case <-ctx.Done():
				C.pgs_picker_cancel(C.uintptr_t(handle))
			case <-pending.done:
			}
		}()
	})
}

//export pgs_picker_done
func pgs_picker_done(id C.uintptr_t, result, diagnostic *C.char) {
	handle := cgo.Handle(id)
	pending := handle.Value().(*pendingPicker)
	defer handle.Delete()
	defer close(pending.done)
	if err := pending.ctx.Err(); err != nil {
		pending.complete(nil, err)
		return
	}
	if diagnostic != nil {
		pending.complete(nil, errors.New(C.GoString(diagnostic)))
		return
	}
	if result == nil {
		pending.complete(nil, ErrCanceled)
		return
	}
	var paths []string
	if err := json.Unmarshal([]byte(C.GoString(result)), &paths); err != nil {
		pending.complete(nil, fmt.Errorf("decode native file selection: %w", err))
		return
	}
	if len(paths) == 0 {
		pending.complete(nil, errors.New("native file selection returned no paths"))
		return
	}
	pending.complete(paths, nil)
}
