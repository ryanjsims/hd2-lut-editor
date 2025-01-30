package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"

	"github.com/gopxl/pixel/v2"
	"github.com/gopxl/pixel/v2/backends/opengl"
	"github.com/gopxl/pixel/v2/ext/atlas"
	"github.com/gopxl/pixelui/v2"
	"github.com/hellflame/argparse"
	"github.com/inkyblackness/imgui-go/v4"
	"github.com/jwalton/go-supportscolor"
	"github.com/ryanjsims/hd2-lut-editor/app"
	"github.com/ryanjsims/hd2-lut-editor/dds"
	_ "github.com/ryanjsims/hd2-lut-editor/dds"
	"github.com/ryanjsims/hd2-lut-editor/hdrColors"
	"github.com/ryanjsims/hd2-lut-editor/openexr"
	"github.com/sqweek/dialog"
	"github.com/x448/float16"
)

var (
	Atlas atlas.Atlas
)

type menuResponse uint8

const (
	menuResponseNone        menuResponse = 0
	menuResponseImageOpen   menuResponse = 1
	menuResponseImageSave   menuResponse = 2
	menuResponseImageSaveAs menuResponse = 3
	menuResponseImageNew    menuResponse = 4
)

type dialogResponse uint8

const (
	dialogResponseNone    dialogResponse = 0
	dialogResponseConfirm dialogResponse = 1
	dialogResponseDeny    dialogResponse = 2
)

const baseTitle string = "Helldiver 2 LUT Editor"

func run() {
	prt := app.NewPrinter(
		supportscolor.Stderr().SupportsColor,
		os.Stderr,
		os.Stderr,
	)

	parser := argparse.NewParser(
		"lut_editor",
		"An HDR pixel editor, made for editing Helldivers 2 material LUTs in floating point image formats",
		&argparse.ParserConfig{
			DisableDefaultShowHelp: true,
		},
	)
	imagePath := parser.String("p", "path", &argparse.Option{
		Positional: true,
		Help:       "Path to an EXR or HDR DDS image to load",
		Required:   false,
	})

	if err := parser.Parse(nil); err != nil {
		if err == argparse.BreakAfterHelpError {
			os.Exit(0)
		}
		prt.Fatalf("%v", err)
	}

	cfg := opengl.WindowConfig{
		Title:  baseTitle,
		Bounds: pixel.R(0, 0, 1024, 768),
		VSync:  true,
	}

	win, err := opengl.NewWindow(cfg)
	if err != nil {
		prt.Fatalf("%v", err)
	}

	clearColor := color.RGBA{
		R: 0x55,
		G: 0x55,
		B: 0x55,
	}

	Atlas.Pack()

	ui := pixelui.New(win, &Atlas, 0)

	var (
		camPos                              = pixel.ZV
		camZoom                             = 24.0
		camZoomSpeed                        = 1.05
		dragStart                           = pixel.ZV
		currColor                           = [4]float32{0.0, 0.0, 0.0, 0.0}
		precision     int32                 = 3
		fileName      string                = ""
		saved         bool                  = true
		refreshSprite bool                  = false
		response      menuResponse          = menuResponseNone
		viewedChannel hdrColors.GraySetting = hdrColors.GraySettingNoAlpha
		lastChannel   hdrColors.GraySetting = hdrColors.GraySettingNoAlpha
	)

	var img image.Image
	if imagePath != nil && len(*imagePath) > 0 {
		img, err = loadImage(*imagePath)

		if err != nil {
			prt.Errorf("Loading image '%s': %v", *imagePath, err)
			img = nil
		} else {
			lastChannel = hdrColors.GraySettingNone
		}
	}

	var pic *pixel.PictureData
	var sprite *pixel.Sprite
	if img != nil {
		pic = pixel.PictureDataFromImage(img)
		sprite = pixel.NewSprite(pic, pic.Bounds())
	}

	if imagePath != nil {
		fileName = *imagePath
	}

	getPixelCoords := func(camera pixel.Matrix, spriteCenter pixel.Vec) (x, y int) {
		coords := camera.Unproject(win.MousePosition()).Add(spriteCenter)
		x, y = int(math.Floor(coords.X)), int(math.Floor(coords.Y))
		return
	}

	for !win.Closed() {
		ui.NewFrame()
		win.Clear(clearColor)
		if refreshSprite && img != nil {
			refreshSprite = false
			pic = pixel.PictureDataFromImage(img)
			if sprite != nil {
				sprite.Set(pic, pic.Bounds())
			} else {
				sprite = pixel.NewSprite(pic, pic.Bounds())
			}
		}

		cam := pixel.IM.Scaled(camPos, camZoom).Moved(win.Bounds().Center().Sub(camPos))

		if ui.JustPressed(pixel.MouseButtonMiddle) {
			dragStart = cam.Unproject(win.MousePosition())
		} else if ui.Pressed(pixel.MouseButtonMiddle) {
			tempCamPos := camPos.Sub(cam.Unproject(win.MousePosition()).Sub(dragStart))
			cam = pixel.IM.Scaled(tempCamPos, camZoom).Moved(win.Bounds().Center().Sub(tempCamPos))
		} else if ui.JustReleased(pixel.MouseButtonMiddle) {
			camPos = camPos.Sub(cam.Unproject(win.MousePosition()).Sub(dragStart))
			cam = pixel.IM.Scaled(camPos, camZoom).Moved(win.Bounds().Center().Sub(camPos))
		}

		if ui.Pressed(pixel.MouseButtonRight) && sprite != nil {
			x, y := getPixelCoords(cam, sprite.Frame().Center())
			y = img.Bounds().Dy() - y - 1
			if x < img.Bounds().Dx() && y < img.Bounds().Dy() && x >= 0 && y >= 0 {
				grayable, ok := getGrayable(img)
				if ok {
					grayable.SetGray(hdrColors.GraySettingNone)
				}
				pxColor := img.At(x, y)
				currColor = hdrColorToFloats(prt, pxColor, img.ColorModel())
				if ok {
					grayable.SetGray(viewedChannel)
				}
			}
		}

		if ui.Pressed(pixel.MouseButtonLeft) && sprite != nil {
			x, y := getPixelCoords(cam, sprite.Frame().Center())
			y = img.Bounds().Dy() - y - 1
			if x < img.Bounds().Dx() && y < img.Bounds().Dy() && x >= 0 && y >= 0 {
				setHDRFromFloats(x, y, currColor, img)
				refreshSprite = true
				saved = false
			}
		}

		win.SetMatrix(cam)
		if sprite != nil {
			sprite.Draw(win, pixel.IM)
		}

		nextResponse := showMainMenuBar()
		if nextResponse != menuResponseNone {
			response = nextResponse
		}

		saveFile := func() {
			out, err := os.OpenFile(fileName, os.O_CREATE, os.ModeType)
			if err != nil {
				prt.Errorf("failed to save: %v", err)
				return
			}
			defer out.Close()
			if filepath.Ext(fileName) == ".exr" {
				err = openexr.WriteHDR(out, img)
			} else if filepath.Ext(fileName) == ".dds" {
				err = dds.WriteHDR(out, img)
			} else {
				prt.Errorf("only saving to .exr or .dds implemented currently")
				return
			}
			if err != nil {
				prt.Errorf("failed to write img to %s: %v", fileName, err)
				return
			}
			saved = true
		}

		switch response {
		case menuResponseImageNew:
			resp := confirmationDialog(win, "Create new file?", "New File", "Confirm", "Cancel")
			if resp == dialogResponseNone {
				break
			}
			response = menuResponseNone
			if resp == dialogResponseConfirm {
				img = hdrColors.NewNRGBA128FImage(img.Bounds())
				refreshSprite = true
				saved = false
				fileName = "(new)"
			}
		case menuResponseImageSave:
			response = menuResponseNone
			go saveFile()
		case menuResponseImageOpen:
			go func() {
				nextFileName, err := dialog.File().Filter("DDS or EXR files", "dds", "exr").Load()
				if err == dialog.ErrCancelled {
					return
				} else if err != nil {
					prt.Errorf("%v", err)
					return
				}
				nextImg, err := loadImage(nextFileName)
				if err != nil {
					prt.Errorf("Failed to load '%s': %v", nextFileName, err)
					return
				}
				fileName = nextFileName
				img = nextImg
				refreshSprite = true
				lastChannel = hdrColors.GraySettingNone
			}()
			response = menuResponseNone
		case menuResponseImageSaveAs:
			go func() {
				nextFileName, err := dialog.File().Filter("DDS or EXR files", "dds", "exr").Save()
				if err == dialog.ErrCancelled {
					return
				} else if err != nil {
					prt.Errorf("%v", err)
					return
				}
				fileName = nextFileName
				saveFile()
			}()
			response = menuResponseNone
		default:
			// Do nothing
		}

		imgui.BeginV("Color", nil, imgui.WindowFlagsAlwaysAutoResize)
		{
			format := fmt.Sprintf("%%.%df", precision)
			imgui.ColorEdit4V("Color", &currColor, imgui.ColorEditFlagsFloat|imgui.ColorEditFlagsHDR|imgui.ColorEditFlagsNoInputs)
			imgui.DragFloatV("Red", &currColor[0], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
			imgui.DragFloatV("Green", &currColor[1], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
			imgui.DragFloatV("Blue", &currColor[2], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
			imgui.DragFloatV("Alpha", &currColor[3], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
			imgui.InputInt("Precision", &precision)
			precision = min(max(precision, 0), 10)
		}
		imgui.End()

		imgui.BeginV("Channel(s)", nil, imgui.WindowFlagsAlwaysAutoResize)
		{
			imgui.RadioButtonInt("RGB", (*int)(&viewedChannel), int(hdrColors.GraySettingNoAlpha))
			imgui.RadioButtonInt("RGBA", (*int)(&viewedChannel), int(hdrColors.GraySettingNone))
			imgui.RadioButtonInt("Red", (*int)(&viewedChannel), int(hdrColors.GraySettingRed))
			imgui.RadioButtonInt("Green", (*int)(&viewedChannel), int(hdrColors.GraySettingBlue))
			imgui.RadioButtonInt("Blue", (*int)(&viewedChannel), int(hdrColors.GraySettingGreen))
			imgui.RadioButtonInt("Alpha   ", (*int)(&viewedChannel), int(hdrColors.GraySettingAlpha))
		}
		imgui.End()

		ui.Draw(win)

		modified := ""
		if !saved {
			modified = "*"
		}
		if lastChannel != viewedChannel {
			grayable, ok := getGrayable(img)

			if ok {
				grayable.SetGray(viewedChannel)
			} else {
				prt.Errorf("failed to set gray")
			}
			lastChannel = viewedChannel
			refreshSprite = true
		}
		win.SetTitle(fmt.Sprintf("%s - %s%s", baseTitle, fileName, modified))

		camZoom *= math.Pow(camZoomSpeed, ui.MouseScroll().Y)

		win.Update()
	}
}

func getGrayable(img image.Image) (hdrColors.Grayable, bool) {
	var grayable hdrColors.Grayable
	var ok bool
	var ddsImg *dds.DDS
	switch img.ColorModel() {
	case hdrColors.NRGBA128FModel:
		grayable, ok = img.(*hdrColors.NRGBA128FImage)
		if ok {
			break
		}
		ddsImg, ok = img.(*dds.DDS)
		if ok {
			grayable, ok = ddsImg.Image.(*hdrColors.NRGBA128FImage)
		}
	case hdrColors.NRGBA64FModel:
		grayable, ok = img.(*hdrColors.NRGBA64FImage)
		if ok {
			break
		}
		ddsImg, ok = img.(*dds.DDS)
		if ok {
			grayable, ok = ddsImg.Image.(*hdrColors.NRGBA64FImage)
		}
	case hdrColors.NRGBA128UModel:
		grayable, ok = img.(*hdrColors.NRGBA128UImage)
		if ok {
			break
		}
		ddsImg, ok = img.(*dds.DDS)
		if ok {
			grayable, ok = ddsImg.Image.(*hdrColors.NRGBA128UImage)
		}
	}
	return grayable, ok
}

func loadImage(path string) (image.Image, error) {
	im, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer im.Close()

	var img image.Image
	if filepath.Ext(path) == ".exr" {
		bufR := bufio.NewReader(im)
		exr, err := openexr.LoadOpenEXR(*bufR)
		if err != nil {
			return nil, err
		}
		img, err = exr.HdrImage()
	} else {
		img, _, err = image.Decode(im)
	}
	return img, err
}

func textCentered(text string) {
	windowWidth := imgui.WindowWidth()
	textWidth := imgui.CalcTextSize(text, false, windowWidth).X

	imgui.SetCursorPos(imgui.Vec2{
		X: (windowWidth - textWidth) * 0.5,
		Y: imgui.CursorPosY(),
	})
	imgui.Text(text)
}

func setHDRFromFloats(x, y int, currColor [4]float32, img image.Image) {
	switch img.ColorModel() {
	case hdrColors.NRGBA128FModel:
		color := hdrColors.NRGBA128F{
			R: currColor[0],
			G: currColor[1],
			B: currColor[2],
			A: currColor[3],
		}
		hdr, ok := img.(*hdrColors.NRGBA128FImage)
		if ok {
			hdr.Set(x, y, color)
		} else {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok := ddsImg.Image.(*hdrColors.NRGBA128FImage)
			if !ok {
				break
			}
			hdr.Set(x, y, color)
		}
	case hdrColors.NRGBA128UModel:
		color := hdrColors.NRGBA128U{
			R: uint32(currColor[0] * float32(0xFFFFFFFF)),
			G: uint32(currColor[1] * float32(0xFFFFFFFF)),
			B: uint32(currColor[2] * float32(0xFFFFFFFF)),
			A: uint32(currColor[3] * float32(0xFFFFFFFF)),
		}
		hdr, ok := img.(*hdrColors.NRGBA128UImage)
		if ok {
			hdr.Set(x, y, color)
		} else {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok := ddsImg.Image.(*hdrColors.NRGBA128UImage)
			if !ok {
				break
			}
			hdr.Set(x, y, color)
		}
	case hdrColors.NRGBA64FModel:
		color := hdrColors.NRGBA64F{
			R: float16.Fromfloat32(currColor[0]),
			G: float16.Fromfloat32(currColor[1]),
			B: float16.Fromfloat32(currColor[2]),
			A: float16.Fromfloat32(currColor[3]),
		}
		hdr, ok := img.(*hdrColors.NRGBA64FImage)
		if ok {
			hdr.Set(x, y, color)
		} else {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok := ddsImg.Image.(*hdrColors.NRGBA64FImage)
			if !ok {
				break
			}
			hdr.Set(x, y, color)
		}
	}
}

func hdrColorToFloats(prt *app.Printer, pxColor color.Color, colorModel color.Model) [4]float32 {
	var color [4]float32
	switch colorModel {
	case hdrColors.NRGBA128FModel:
		px, ok := pxColor.(hdrColors.NRGBA128F)
		if !ok {
			prt.Errorf("failed to get NRGBA128F color from img")
		} else {
			color[0] = px.R
			color[1] = px.G
			color[2] = px.B
			color[3] = px.A
		}
	case hdrColors.NRGBA64FModel:
		px, ok := pxColor.(hdrColors.NRGBA64F)
		if !ok {
			prt.Errorf("failed to get NRGBA64F color from img")
		} else {
			color[0] = px.R.Float32()
			color[1] = px.G.Float32()
			color[2] = px.B.Float32()
			color[3] = px.A.Float32()
		}
	case hdrColors.NRGBA64FModel:
		px, ok := pxColor.(hdrColors.NRGBA128U)
		if !ok {
			prt.Errorf("failed to get NRGBA128U color from img")
		} else {
			color[0] = float32(px.R) / float32(0xffffffff)
			color[1] = float32(px.G) / float32(0xffffffff)
			color[2] = float32(px.B) / float32(0xffffffff)
			color[3] = float32(px.A) / float32(0xffffffff)
		}
	default:
		prt.Errorf("bad colormodel %v", colorModel)
	}
	return color
}

func confirmationDialog(win *opengl.Window, text, title, confirm, deny string) dialogResponse {
	response := dialogResponseNone
	popupSize := imgui.Vec2{
		X: 0.2 * float32(win.Bounds().W()),
		Y: 0.2 * float32(win.Bounds().H()),
	}
	imgui.SetNextWindowPos(imgui.Vec2{
		X: 0.5 * (float32(win.Bounds().W()) - popupSize.X),
		Y: 0.5 * (float32(win.Bounds().H()) - popupSize.Y),
	})
	imgui.SetNextWindowSize(popupSize)
	imgui.BeginV(title, nil, imgui.WindowFlagsNoMove|imgui.WindowFlagsNoResize|imgui.WindowFlagsNoCollapse)

	imgui.SetCursorPos(imgui.Vec2{
		X: imgui.CursorPosX(),
		Y: popupSize.Y * .33,
	})
	textCentered(text)
	imgui.SetCursorPos(imgui.Vec2{
		X: imgui.CursorPosX(),
		Y: popupSize.Y * .75,
	})

	buttonSize := imgui.Vec2{
		X: popupSize.X * 0.3,
		Y: popupSize.Y * 0.2,
	}

	if imgui.ButtonV(confirm, buttonSize) {
		response = dialogResponseConfirm
	}
	imgui.SameLine()
	imgui.SetCursorPos(imgui.Vec2{
		X: popupSize.X * 0.65,
		Y: imgui.CursorPosY(),
	})
	if imgui.ButtonV(deny, buttonSize) {
		response = dialogResponseDeny
	}
	imgui.End()
	return response
}

func showMainMenuBar() menuResponse {
	response := menuResponseNone
	if imgui.BeginMainMenuBar() {
		if imgui.BeginMenu("File") {
			response = showFileMenu()
			imgui.EndMenu()
		}
		imgui.EndMainMenuBar()
	}
	return response
}

func showFileMenu() menuResponse {
	response := menuResponseNone
	if imgui.MenuItem("New") {
		response = menuResponseImageNew
	}
	if imgui.MenuItem("Open...") {
		response = menuResponseImageOpen
	}
	if imgui.MenuItem("Save") {
		response = menuResponseImageSave
	}
	if imgui.MenuItem("Save As...") {
		response = menuResponseImageSaveAs
	}
	return response
}

func main() {
	opengl.Run(run)
}
