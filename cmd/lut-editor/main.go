package main

import (
	"bufio"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"

	"github.com/gopxl/pixel/v2"
	"github.com/gopxl/pixel/v2/backends/opengl"
	"github.com/gopxl/pixel/v2/ext/atlas"
	"github.com/gopxl/pixel/v2/ext/imdraw"
	"github.com/gopxl/pixelui/v2"
	"github.com/hellflame/argparse"
	"github.com/inkyblackness/imgui-go/v4"
	"github.com/jwalton/go-supportscolor"
	"github.com/ryanjsims/hd2-lut-editor/app"
	"github.com/ryanjsims/hd2-lut-editor/dds"
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
	menuResponseNone         menuResponse = 0
	menuResponseImageOpen    menuResponse = 1
	menuResponseImageSave    menuResponse = 2
	menuResponseImageSaveAs  menuResponse = 3
	menuResponseImageNew     menuResponse = 4
	menuResponseViewChannels menuResponse = 5
	menuResponseViewColor    menuResponse = 6
	menuResponseViewHelp     menuResponse = 7
	menuResponseViewGrid     menuResponse = 8
)

type dialogResponse uint8

const (
	dialogResponseNone    dialogResponse = 0
	dialogResponseConfirm dialogResponse = 1
	dialogResponseDeny    dialogResponse = 2
)

type newImageState uint8

const (
	newImageStateNone     newImageState = 0
	newImageStateConfirm  newImageState = 1
	newImageStateSettings newImageState = 2
)

type newImageStateMachine struct {
	State      newImageState
	Width      int32
	Height     int32
	ModelIndex int
}

const baseTitle string = "Helldiver 2 LUT Editor"

func run() {
	logFile, err := os.OpenFile("lut-editor.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logFile = os.Stderr
	} else {
		defer logFile.Close()
	}

	prt := app.NewPrinter(
		supportscolor.SupportsColor(logFile.Fd(), supportscolor.SniffFlagsOption(true)).SupportsColor,
		logFile,
		logFile,
	)

	defer func() {
		if r := recover(); r != nil {
			prt.Errorf("panic: %w\n%s", r, debug.Stack())
		}
	}()

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

	if err = parser.Parse(nil); err != nil {
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
		camPos                                = pixel.ZV
		camZoom                               = 24.0
		camZoomSpeed                          = 1.05
		dragStart                             = pixel.ZV
		currColor                             = [4]float32{0.0, 0.0, 0.0, 0.0}
		precision       int32                 = 3
		fileName        string                = ""
		saved           bool                  = true
		refreshSprite   bool                  = false
		channelsVisible bool                  = true
		colorVisible    bool                  = true
		gridVisible     bool                  = true
		response        menuResponse          = menuResponseNone
		viewedChannel   hdrColors.GraySetting = hdrColors.GraySettingNoAlpha
		lastChannel     hdrColors.GraySetting = hdrColors.GraySettingNoAlpha
		smNewImage      newImageStateMachine
	)

	smNewImage = newImageStateMachine{
		State:      newImageStateNone,
		Width:      23,
		Height:     8,
		ModelIndex: 0,
	}

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
			x, y := getPixelCoords(cam, sprite.Frame().Center(), win.MousePosition())
			currColor = getImgColorAtCoords(prt, img, x, y, viewedChannel)
		}

		if ui.Pressed(pixel.MouseButtonLeft) && sprite != nil {
			x, y := getPixelCoords(cam, sprite.Frame().Center(), win.MousePosition())
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

		nextResponse := showMainMenuBar(img, channelsVisible, colorVisible, gridVisible)
		if nextResponse != menuResponseNone {
			response = nextResponse
		}

		switch response {
		case menuResponseImageNew:
			if smNewImage.State == newImageStateNone && len(fileName) != 0 {
				smNewImage.State = newImageStateConfirm
			} else if smNewImage.State == newImageStateNone {
				smNewImage.State = newImageStateSettings
			}
			smNewImage.handleNewImageStateMachine(win, &img, &refreshSprite, &saved, &fileName, &lastChannel)
			if smNewImage.State == newImageStateNone {
				response = menuResponseNone
			}
		case menuResponseImageSave:
			response = menuResponseNone
			if fileName == "(new)" || len(fileName) == 0 {
				go saveFileAs(prt, &fileName, img, &saved)
			} else {
				go saveFile(prt, fileName, img, &saved)
			}
		case menuResponseImageOpen:
			response = menuResponseNone
			go openFile(prt, &fileName, &img, &refreshSprite, &lastChannel)
		case menuResponseImageSaveAs:
			response = menuResponseNone
			go saveFileAs(prt, &fileName, img, &saved)
		case menuResponseViewChannels:
			response = menuResponseNone
			channelsVisible = !channelsVisible
		case menuResponseViewColor:
			response = menuResponseNone
			colorVisible = !colorVisible
		case menuResponseViewGrid:
			response = menuResponseNone
			gridVisible = !gridVisible
		default:
			// Do nothing
			response = menuResponseNone
		}

		if gridVisible && sprite != nil {
			drawGrid(win, camZoom, sprite.Frame())
		}

		if colorVisible {
			drawColorWindow(&precision, &currColor, &colorVisible)
		}
		if channelsVisible {
			drawChannelWindow(&viewedChannel, &channelsVisible)
		}

		center := pixel.ZV
		if sprite != nil {
			center = sprite.Frame().Center()
		}
		hovX, hovY := getPixelCoords(cam, center, win.MousePosition())
		hovColor := getImgColorAtCoords(prt, img, hovX, hovY, viewedChannel)
		hovY = -hovY - 1
		if img != nil {
			hovY += img.Bounds().Dy()
		}
		drawStatusBar(hovX+1, hovY+1, hovColor)

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

func saveFile(prt *app.Printer, fileName string, img image.Image, saved *bool) {
	out, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY, 0644)
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
	length, err := out.Seek(0, io.SeekCurrent)
	if err == nil {
		out.Truncate(length)
	}
	*saved = true
}

func saveFileAs(prt *app.Printer, fileName *string, img image.Image, saved *bool) {
	nextFileName, err := dialog.File().Filter("DDS or EXR files", "dds", "exr").Save()
	if err == dialog.ErrCancelled {
		return
	} else if err != nil {
		prt.Errorf("%v", err)
		return
	}
	*fileName = nextFileName
	saveFile(prt, *fileName, img, saved)
}

func openFile(prt *app.Printer, fileName *string, img *image.Image, refreshSprite *bool, lastChannel *hdrColors.GraySetting) {
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
	*fileName = nextFileName
	*img = nextImg
	*refreshSprite = true
	*lastChannel = hdrColors.GraySettingNone
}

func getPixelCoords(camera pixel.Matrix, spriteCenter pixel.Vec, mousePosition pixel.Vec) (x, y int) {
	coords := camera.Unproject(mousePosition).Add(spriteCenter)
	x, y = int(math.Floor(coords.X)), int(math.Floor(coords.Y))
	return
}

func getImgColorAtCoords(prt *app.Printer, img image.Image, x, y int, viewedChannel hdrColors.GraySetting) [4]float32 {
	if img == nil {
		return [4]float32{}
	}
	y = img.Bounds().Dy() - y - 1
	if x < img.Bounds().Dx() && y < img.Bounds().Dy() && x >= 0 && y >= 0 {
		grayable, ok := getGrayable(img)
		if ok {
			grayable.SetGray(hdrColors.GraySettingNone)
		}
		pxColor := img.At(x, y)
		if ok {
			grayable.SetGray(viewedChannel)
		}
		return hdrColorToFloats(prt, pxColor, img.ColorModel())
	}
	return [4]float32{}
}

func drawChannelWindow(viewedChannel *hdrColors.GraySetting, visible *bool) {
	imgui.BeginV("Channel(s)", visible, imgui.WindowFlagsAlwaysAutoResize|imgui.WindowFlagsNoCollapse)
	{
		imgui.RadioButtonInt("RGB", (*int)(viewedChannel), int(hdrColors.GraySettingNoAlpha))
		imgui.RadioButtonInt("RGBA", (*int)(viewedChannel), int(hdrColors.GraySettingNone))
		imgui.RadioButtonInt("Red", (*int)(viewedChannel), int(hdrColors.GraySettingRed))
		imgui.RadioButtonInt("Green", (*int)(viewedChannel), int(hdrColors.GraySettingBlue))
		imgui.RadioButtonInt("Blue", (*int)(viewedChannel), int(hdrColors.GraySettingGreen))
		imgui.RadioButtonInt("Alpha   ", (*int)(viewedChannel), int(hdrColors.GraySettingAlpha))
	}
	imgui.End()
}

func drawColorWindow(precision *int32, currColor *([4]float32), visible *bool) {
	imgui.BeginV("Color", visible, imgui.WindowFlagsAlwaysAutoResize|imgui.WindowFlagsNoCollapse)
	{
		format := fmt.Sprintf("%%.%df", *precision)
		imgui.ColorEdit4V("Color", currColor, imgui.ColorEditFlagsFloat|imgui.ColorEditFlagsHDR|imgui.ColorEditFlagsNoInputs)
		imgui.DragFloatV("Red", &currColor[0], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
		imgui.DragFloatV("Green", &currColor[1], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
		imgui.DragFloatV("Blue", &currColor[2], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
		imgui.DragFloatV("Alpha", &currColor[3], 0.01, 0.0, 0.0, format, imgui.SliderFlagsNone)
		imgui.InputInt("Precision", precision)
		*precision = min(max(*precision, 0), 10)
	}
	imgui.End()
}

func drawGrid(win *opengl.Window, camZoom float64, spriteFrame pixel.Rect) {
	grid := imdraw.New(nil)
	gridColor := pixel.RGBA{
		R: 0.5,
		G: 0.5,
		B: 0.5,
		A: 0.25,
	}

	pixels := spriteFrame.Size()
	lineWidth := 1.0 / camZoom
	lineSpacing := int(max(1, math.Pow(2.0, math.Log2(lineWidth)+3.5)))
	for line := 0; line <= int(pixels.X); line += lineSpacing {
		grid.Color = gridColor
		grid.Push(pixel.Vec{X: float64(line) - pixels.X/2, Y: -pixels.Y / 2})
		grid.Push(pixel.Vec{X: float64(line) - pixels.X/2, Y: pixels.Y / 2})
		grid.Line(lineWidth)
		grid.Reset()
	}
	for line := 0; line <= int(pixels.Y); line += lineSpacing {
		grid.Color = gridColor
		grid.Push(pixel.Vec{X: -pixels.X / 2, Y: float64(line) - pixels.Y/2})
		grid.Push(pixel.Vec{X: pixels.X / 2, Y: float64(line) - pixels.Y/2})
		grid.Line(lineWidth)
		grid.Reset()
	}
	grid.Draw(win)
}

func drawStatusBar(x, y int, color [4]float32) {
	viewport := imgui.MainViewport()
	imgui.SetNextWindowPos(imgui.Vec2{
		X: viewport.Pos().X,
		Y: viewport.Pos().Y + viewport.Size().Y - imgui.FrameHeight(),
	})

	imgui.SetNextWindowSize(imgui.Vec2{
		X: viewport.Size().X,
		Y: imgui.FrameHeight(),
	})

	flags := (imgui.WindowFlagsNoDecoration | imgui.WindowFlagsNoInputs |
		imgui.WindowFlagsNoMove | imgui.WindowFlagsNoScrollWithMouse |
		imgui.WindowFlagsNoSavedSettings | imgui.WindowFlagsNoBringToFrontOnFocus |
		imgui.WindowFlagsNoBackground | imgui.WindowFlagsMenuBar)

	if imgui.BeginV("StatusBar", nil, flags) {
		if imgui.BeginMenuBar() {
			imgui.Textf("X: %d Y: %d RGBA: (%3.3f, %3.3f, %3.3f, %3.3f)", x, y, color[0], color[1], color[2], color[3])
			imgui.EndMenuBar()
		}
		imgui.End()
	}
}

func (st *newImageStateMachine) handleNewImageStateMachine(win *opengl.Window, img *image.Image, refreshSprite *bool, saved *bool, fileName *string, lastChannel *hdrColors.GraySetting) {
	windowSize := imgui.Vec2{
		X: 0.2 * float32(win.Bounds().W()),
		Y: 0.2 * float32(win.Bounds().H()),
	}
	switch st.State {
	case newImageStateConfirm:
		centerWindow(win, windowSize)
		resp := confirmationDialog(windowSize, "Create new file?", "New File", "Confirm", "Cancel")
		switch resp {
		case dialogResponseDeny:
			st.State = newImageStateNone
		case dialogResponseConfirm:
			st.State = newImageStateSettings
		default:
		}
		return
	case newImageStateSettings:
		//var model color.Model
		centerWindow(win, windowSize)
		resp := newImageDialog(st, windowSize)
		st.Width = max(st.Width, 1)
		st.Height = max(st.Height, 1)
		if resp == dialogResponseNone {
			return
		}
		if resp == dialogResponseConfirm {
			switch st.ModelIndex {
			case 0:
				*img = hdrColors.NewNRGBA128FImage(image.Rect(0, 0, int(st.Width), int(st.Height)))
			case 1:
				*img = hdrColors.NewNRGBA64FImage(image.Rect(0, 0, int(st.Width), int(st.Height)))
			}
			*refreshSprite = true
			*saved = false
			*fileName = "(new)"
			*lastChannel = hdrColors.GraySettingNone
		}
		st.State = newImageStateNone
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
		var exr *openexr.OpenEXR
		bufR := bufio.NewReader(im)
		exr, err = openexr.LoadOpenEXR(*bufR)
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

func centerWindow(win *opengl.Window, windowSize imgui.Vec2) {
	imgui.SetNextWindowPos(imgui.Vec2{
		X: 0.5 * (float32(win.Bounds().W()) - windowSize.X),
		Y: 0.5 * (float32(win.Bounds().H()) - windowSize.Y),
	})
	imgui.SetNextWindowSize(windowSize)
}

func confirmationDialog(windowSize imgui.Vec2, text, title, confirm, deny string) dialogResponse {
	response := dialogResponseNone
	imgui.BeginV(title, nil, imgui.WindowFlagsNoMove|imgui.WindowFlagsNoResize|imgui.WindowFlagsNoCollapse)

	imgui.SetCursorPos(imgui.Vec2{
		X: imgui.CursorPosX(),
		Y: windowSize.Y * .33,
	})
	textCentered(text)
	imgui.SetCursorPos(imgui.Vec2{
		X: imgui.CursorPosX(),
		Y: windowSize.Y * .75,
	})

	buttonSize := imgui.Vec2{
		X: windowSize.X * 0.3,
		Y: windowSize.Y * 0.2,
	}

	if imgui.ButtonV(confirm, buttonSize) {
		response = dialogResponseConfirm
	}
	imgui.SameLine()
	imgui.SetCursorPos(imgui.Vec2{
		X: windowSize.X * 0.65,
		Y: imgui.CursorPosY(),
	})
	if imgui.ButtonV(deny, buttonSize) {
		response = dialogResponseDeny
	}
	imgui.End()
	return response
}

func newImageDialog(st *newImageStateMachine, windowSize imgui.Vec2) dialogResponse {
	var resp dialogResponse = dialogResponseNone
	imgui.BeginV("New file settings", nil, imgui.WindowFlagsNoMove|imgui.WindowFlagsNoResize|imgui.WindowFlagsNoCollapse)
	imgui.InputInt("Width", &st.Width)
	imgui.InputInt("Height", &st.Height)
	imgui.RadioButtonInt("Float", &st.ModelIndex, 0)
	imgui.SameLine()
	imgui.RadioButtonInt("Half", &st.ModelIndex, 1)
	buttonSize := imgui.Vec2{
		X: windowSize.X * 0.3,
		Y: windowSize.Y * 0.2,
	}
	imgui.SetCursorPos(imgui.Vec2{
		X: imgui.CursorPosX(),
		Y: windowSize.Y * .75,
	})
	if imgui.ButtonV("OK", buttonSize) {
		resp = dialogResponseConfirm
	}
	imgui.SameLine()
	imgui.SetCursorPos(imgui.Vec2{
		X: windowSize.X * 0.65,
		Y: imgui.CursorPosY(),
	})
	if imgui.ButtonV("Cancel", buttonSize) {
		resp = dialogResponseDeny
	}
	imgui.End()
	return resp
}

func showMainMenuBar(img image.Image, channelsVisible, colorVisible, gridVisible bool) menuResponse {
	response := menuResponseNone
	if imgui.BeginMainMenuBar() {
		if imgui.BeginMenu("File") {
			response = showFileMenu(img)
			imgui.EndMenu()
		}
		if imgui.BeginMenu("View") {
			response = showViewMenu(channelsVisible, colorVisible, gridVisible)
			imgui.EndMenu()
		}
		imgui.EndMainMenuBar()
	}
	return response
}

func showFileMenu(img image.Image) menuResponse {
	response := menuResponseNone
	if imgui.MenuItem("New") {
		response = menuResponseImageNew
	}
	if imgui.MenuItem("Open...") {
		response = menuResponseImageOpen
	}
	if imgui.MenuItemV("Save", "", false, img != nil) {
		response = menuResponseImageSave
	}
	if imgui.MenuItemV("Save As...", "", false, img != nil) {
		response = menuResponseImageSaveAs
	}
	return response
}

func showViewMenu(channelsVisible, colorVisible, gridVisible bool) menuResponse {
	response := menuResponseNone
	if imgui.MenuItemV("Channels", "", channelsVisible, true) {
		response = menuResponseViewChannels
	}
	if imgui.MenuItemV("Color", "", colorVisible, true) {
		response = menuResponseViewColor
	}
	if imgui.MenuItemV("Grid", "", gridVisible, true) {
		response = menuResponseViewGrid
	}
	return response
}

func main() {
	opengl.Run(run)
}
