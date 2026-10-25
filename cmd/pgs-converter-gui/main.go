// Command pgs-converter-gui runs the desktop application without a WebView.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"os"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"

	"github.com/ninghaofeng/pgs_converter/internal/cli"
	"github.com/ninghaofeng/pgs_converter/internal/desktop"
)

func main() {
	version := flag.Bool("version", false, "print version without opening a window")
	smoke := flag.String("smoke-test", "", "save a native window screenshot to this PNG path, then exit")
	language := flag.String("language", "", "UI language: en or zh-CN (saved; default follows system)")
	flag.Parse()
	if *version {
		fmt.Println(cli.Version)
		return
	}
	if *language != "" && *language != "en" && *language != "zh-CN" {
		fmt.Fprintln(os.Stderr, "-language must be en or zh-CN")
		os.Exit(2)
	}
	id := "io.github.ninghaofeng.pgs-converter"
	if *smoke != "" {
		// Automated captures must not overwrite the user's normal preferences.
		id += ".smoke"
	}
	a := app.NewWithID(id)
	if *language != "" {
		a.Preferences().SetString("desktop-language", *language)
	}
	w := desktop.New(a, cli.Version, flag.Args()...)
	code := 0
	if *smoke != "" {
		a.Lifecycle().SetOnStarted(func() {
			go func() {
				time.Sleep(2 * time.Second)
				fyne.Do(func() {
					file, err := os.Create(*smoke)
					if err == nil {
						err = png.Encode(file, w.Canvas().Capture())
						if closeErr := file.Close(); err == nil {
							err = closeErr
						}
					}
					if err != nil {
						fmt.Fprintln(os.Stderr, "GUI smoke test:", err)
						code = 1
					}
					w.Close()
				})
			}()
		})
	}
	w.ShowAndRun()
	if code != 0 {
		os.Exit(code)
	}
}
