package main

import (
	"image/color"
	"os"

	"github.com/gopxl/pixel/v2"
	"github.com/gopxl/pixel/v2/backends/opengl"
	"github.com/jwalton/go-supportscolor"
	"github.com/ryanjsims/hd2-lut-editor/app"
)

func run() {
	prt := app.NewPrinter(
		supportscolor.Stderr().SupportsColor,
		os.Stderr,
		os.Stderr,
	)

	cfg := opengl.WindowConfig{
		Title:  "Helldiver 2 LUT Editor",
		Bounds: pixel.R(0, 0, 1024, 768),
		VSync:  true,
	}

	win, err := opengl.NewWindow(cfg)
	if err != nil {
		prt.Fatalf("%v", err)
	}

	win.Clear(color.RGBA{
		R: 0x55,
		G: 0x55,
		B: 0x55,
	})

	for !win.Closed() {
		win.Update()
	}
}

func main() {
	opengl.Run(run)
}
