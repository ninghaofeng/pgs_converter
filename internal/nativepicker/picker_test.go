package nativepicker

import (
	"context"
	"errors"
	"testing"
)

func TestCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	Open(ctx, nil, Request{}, func(paths []string, err error) {
		calls++
		if len(paths) != 0 || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled request returned %v, %v", paths, err)
		}
	})
	if calls != 1 {
		t.Fatalf("completion ran %d times", calls)
	}
}

func TestMissingNativeWindow(t *testing.T) {
	calls := 0
	Open(context.Background(), nil, Request{}, func(paths []string, err error) {
		calls++
		if len(paths) != 0 || err == nil {
			t.Fatalf("missing native window was silently accepted: %v, %v", paths, err)
		}
	})
	if calls != 1 {
		t.Fatalf("completion ran %d times", calls)
	}
}
