package desktop

import (
	"bytes"
	"image/png"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

type iconApp struct {
	fyne.App
	icon fyne.Resource
}

func (a *iconApp) SetIcon(icon fyne.Resource) { a.icon = icon }
func (a *iconApp) Icon() fyne.Resource        { return a.icon }
func (a *iconApp) NewWindow(title string) fyne.Window {
	return &iconWindow{Window: a.App.NewWindow(title)}
}

type iconWindow struct {
	fyne.Window
	icon fyne.Resource
}

func (w *iconWindow) SetIcon(icon fyne.Resource) { w.icon = icon }
func (w *iconWindow) Icon() fyne.Resource        { return w.icon }

func TestApplicationAndWindowIcons(t *testing.T) {
	a := &iconApp{App: test.NewApp()}
	defer a.Quit()
	w := New(a, "test")
	defer w.Close()
	for _, resource := range []fyne.Resource{a.Icon(), w.Icon()} {
		if resource == nil || !bytes.Equal(resource.Content(), iconPNG) {
			t.Fatal("application and window must use the packaged icon")
		}
		config, err := png.DecodeConfig(bytes.NewReader(resource.Content()))
		if err != nil || config.Width != 512 || config.Height != 512 {
			t.Fatalf("embedded window icon is not a 512px PNG: %v", err)
		}
	}
}
