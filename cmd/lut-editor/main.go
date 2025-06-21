package main

import (
	"bufio"
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gopxl/pixel/v2"
	"github.com/gopxl/pixel/v2/backends/opengl"
	"github.com/gopxl/pixel/v2/ext/atlas"
	"github.com/gopxl/pixel/v2/ext/imdraw"
	"github.com/gopxl/pixelui/v2"
	"github.com/hellflame/argparse"
	"github.com/inkyblackness/imgui-go/v4"
	"github.com/jwalton/go-supportscolor"
	"github.com/ryanjsims/hd2-lut-editor/app"
	"github.com/ryanjsims/hd2-lut-editor/clipboard"
	"github.com/ryanjsims/hd2-lut-editor/dds"
	"github.com/ryanjsims/hd2-lut-editor/hdrColors"
	"github.com/ryanjsims/hd2-lut-editor/openexr"
	"github.com/ryanjsims/hd2-lut-editor/types"
	"github.com/sqweek/dialog"
	"github.com/x448/float16"
)

var (
	Atlas atlas.Atlas
)

type lmbTool int

const (
	toolDraw         lmbTool = iota
	toolSelect       lmbTool = iota
	toolMoveSelected lmbTool = iota
)

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
			prt.Errorf("panic: %v\n%s", r, debug.Stack())
		}
	}()

	err = clipboard.Init()
	if err != nil {
		fmt.Println(err)
	}

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
		camPos                   = pixel.ZV
		camZoom                  = 24.0
		camZoomSpeed             = 1.05
		dragStart                = pixel.ZV
		currColor                = [4]float32{0.0, 0.0, 0.0, 0.0}
		precision         int32  = 3
		fileName          string = ""
		saved             bool   = true
		refreshSprites    bool   = false
		channelsVisible   bool   = true
		colorVisible      bool   = true
		gridVisible       bool   = true
		toolsVisible      bool   = true
		newImageConfirm   bool
		newImageWidth     int32                 = 23
		newImageHeight    int32                 = 8
		newImagePrecision int                   = 0
		response          types.MenuResponse    = types.MenuResponseNone
		viewedChannel     hdrColors.GraySetting = hdrColors.GraySettingNoAlpha
		lastChannel       hdrColors.GraySetting = hdrColors.GraySettingNoAlpha
		undoStack         types.UndoRedoStack   = types.UndoRedoStack{
			UndoStack: make([]types.UndoRedoState, 0),
			RedoStack: make([]types.UndoRedoState, 0),
		}
		backgroundTasks = make(types.TaskMap)
		img             image.Image
		pasteImg        image.Image
		pastePic        *pixel.PictureData
		pasteSprite     *pixel.Sprite
		tool            lmbTool = toolDraw
		prevTool        lmbTool = toolDraw
		selection               = pixel.ZR
		selectionStart          = pixel.ZV
		selectionEnd            = pixel.ZV
		selectionOffset         = pixel.ZV
	)

	if imagePath != nil && len(*imagePath) > 0 {
		img, err = loadImage(*imagePath)

		if err != nil {
			prt.Errorf("Loading image '%s': %v", *imagePath, err)
			img = nil
		} else {
			lastChannel = hdrColors.GraySettingNone
			newImageWidth = int32(img.Bounds().Dx())
			newImageHeight = int32(img.Bounds().Dy())
			undoStack.Push("Load File", *imagePath, true, img, currColor, selection)
		}
	}

	newImageConfirm = img == nil

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
		if refreshSprites && img != nil {
			refreshSprites = false
			pic = pixel.PictureDataFromImage(img)
			if sprite != nil {
				sprite.Set(pic, pic.Bounds())
			} else {
				sprite = pixel.NewSprite(pic, pic.Bounds())
			}

			if pasteImg != nil {
				pastePic = pixel.PictureDataFromImage(pasteImg)
				if pasteSprite != nil {
					pasteSprite.Set(pastePic, pastePic.Bounds())
				} else {
					pasteSprite = pixel.NewSprite(pastePic, pastePic.Bounds())
				}
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
			undoStack.DelayedPush(1*time.Second, "Pick Color", &fileName, &saved, &img, &currColor, &selection)
		}

		if ui.Pressed(pixel.MouseButtonLeft) && sprite != nil {
			x, y := getPixelCoords(cam, sprite.Frame().Center(), win.MousePosition())
			y = img.Bounds().Dy() - y - 1
			point := image.Rect(x, y, x, y)
			if point.In(img.Bounds()) {
				switch tool {
				case toolDraw:
					setHDRFromFloats(x, y, currColor, img)
					refreshSprites = true
					saved = false
					undoStack.DelayedPush(1*time.Second, "Draw", &fileName, &saved, &img, &currColor, &selection)
				case toolSelect:
					mousePos := cam.Unproject(win.MousePosition())
					clampedX := math.Max(0, math.Min(float64(x), float64(img.Bounds().Dx())))
					clampedY := math.Max(0, math.Min(float64(y), float64(img.Bounds().Dy())))
					if ui.JustPressed(pixel.MouseButtonLeft) {
						selectionStart = fromPixelCoords(cam, sprite.Frame().Center(), int(clampedX), img.Bounds().Dy()-int(clampedY))
					}
					if selectionStart.X < mousePos.X {
						clampedX = math.Max(0, math.Min(float64(x+1), float64(img.Bounds().Dx())))
					}
					if selectionStart.Y < mousePos.Y {
						clampedY = math.Max(0, math.Min(float64(y+1), float64(img.Bounds().Dy())))
					}
					selectionEnd = fromPixelCoords(cam, sprite.Frame().Center(), int(clampedX), img.Bounds().Dy()-int(clampedY))
					selection.Min = selectionStart
					selection.Max = selectionEnd
					selection = selection.Norm()
					undoStack.DelayedPush(1*time.Second, "Change Selection", &fileName, &saved, &img, &currColor, &selection)
				case toolMoveSelected:
					clampedX := math.Max(0, math.Min(float64(x), float64(img.Bounds().Dx())))
					clampedY := math.Max(0, math.Min(float64(y), float64(img.Bounds().Dy())))
					if ui.JustPressed(pixel.MouseButtonLeft) {
						selectionStart = fromPixelCoords(cam, sprite.Frame().Center(), int(clampedX), img.Bounds().Dy()-int(clampedY))
					}
					selectionEnd = fromPixelCoords(cam, sprite.Frame().Center(), int(clampedX), img.Bounds().Dy()-int(clampedY))
					selectionOffset = selectionEnd.Sub(selectionStart)
					undoStack.DelayedPush(1*time.Second, "Move Selection", &fileName, &saved, &img, &currColor, &selection)
				}
			}
		}
		if ui.JustReleased(pixel.MouseButtonLeft) && selectionOffset != pixel.ZV {
			selection = selection.Moved(selectionOffset)
			selectionOffset = pixel.ZV
		}

		// Undo
		if (ui.Pressed(pixel.KeyLeftControl) || ui.Pressed(pixel.KeyRightControl)) &&
			!(ui.Pressed(pixel.KeyLeftShift) || ui.Pressed(pixel.KeyRightShift)) &&
			ui.JustPressed(pixel.KeyZ) && len(undoStack.UndoStack) > 1 {
			handleUndo(prt, &undoStack, max(0, len(undoStack.UndoStack)-2), &img, &refreshSprites, &lastChannel, &currColor, &selection)
		}
		// Redo
		if (ui.Pressed(pixel.KeyLeftControl) || ui.Pressed(pixel.KeyRightControl)) &&
			(ui.Pressed(pixel.KeyLeftShift) || ui.Pressed(pixel.KeyRightShift)) &&
			ui.JustPressed(pixel.KeyZ) && len(undoStack.RedoStack) > 0 {
			handleRedo(prt, &undoStack, max(0, len(undoStack.RedoStack)-1), &img, &refreshSprites, &lastChannel, &currColor, &selection)
		}

		// Copy shortcut
		if (ui.Pressed(pixel.KeyLeftControl) || ui.Pressed(pixel.KeyRightControl)) &&
			ui.JustPressed(pixel.KeyC) && img != nil && selection != pixel.ZR {
			err := handleCopy(selection, sprite.Frame().Center(), img)
			if err != nil {
				prt.Errorf("failed to copy image: %w", err)
			}
		}

		// Cut shortcut
		if (ui.Pressed(pixel.KeyLeftControl) || ui.Pressed(pixel.KeyRightControl)) &&
			ui.JustPressed(pixel.KeyX) && img != nil && selection != pixel.ZR {
			undoStack.Push("Cut", fileName, saved, img, currColor, selection)
			err := handleCut(selection, sprite.Frame().Center(), img)
			if err != nil {
				prt.Errorf("failed to cut image: %w", err)
			} else {
				saved = false
			}
		}

		// Paste shortcut
		if (ui.Pressed(pixel.KeyLeftControl) || ui.Pressed(pixel.KeyRightControl)) &&
			ui.JustPressed(pixel.KeyV) && img != nil {
			pasteImg, err = clipboard.ReadHDR()
			if err == clipboard.ErrUnavailable {
				// do nothing
			} else if err != nil {
				prt.Errorf("failed to paste image: %w", err)
			} else {
				grayable := pasteImg.(hdrColors.Grayable)
				grayable.SetGray(viewedChannel)
				refreshSprites = true
				prevTool = tool
				tool = toolMoveSelected
				storedSelection, err := clipboard.ReadRect()
				if err == clipboard.ErrUnavailable {
					// do nothing
				} else if err != nil {
					prt.Errorf("failed to paste selection box: %w", err)
				} else {
					imageRect := selectionToImageRect(*storedSelection, sprite.Frame().Center(), img.Bounds().Dy())
					if imageRect.In(img.Bounds()) {
						selection = *storedSelection
					}
				}
			}
		}

		// Finish moving pixels shortcut
		if tool == toolMoveSelected && ui.JustPressed(pixel.KeyEnter) && img != nil && pasteImg != nil {
			undoStack.Push("Finish pixels", fileName, saved, img, currColor, selection)
			handleImageCombine(selection, sprite.Frame().Center(), img, pasteImg)
			refreshSprites = true
			tool = prevTool
			saved = false
			pasteImg = nil
			pastePic = nil
			pasteSprite = nil
		}

		// Cancel moving pixels shortcut
		if tool == toolMoveSelected && ui.JustPressed(pixel.KeyEscape) && img != nil && pasteImg != nil {
			tool = prevTool
			pasteImg = nil
			pastePic = nil
			pasteSprite = nil
		}

		win.SetMatrix(cam)
		if sprite != nil {
			sprite.Draw(win, pixel.IM)
		}
		if tool == toolSelect && pasteSprite != nil {
			pasteImg = nil
			pastePic = nil
			pasteSprite = nil
		}
		if pasteSprite != nil {
			pasteSprite.Draw(win, pixel.IM.Moved(selection.Moved(selectionOffset).Center()))
		}

		nextResponse, index := showMainMenuBar(img, channelsVisible, colorVisible, gridVisible, toolsVisible, &undoStack)
		if nextResponse != types.MenuResponseNone {
			response = nextResponse
		}

		switch response {
		case types.MenuResponseImageNew:
			if createNewImage(&img, &newImageConfirm, &refreshSprites, &saved, &fileName, &lastChannel, &newImageWidth, &newImageHeight, &newImagePrecision) {
				response = types.MenuResponseNone
				if img != nil {
					undoStack.Clear()
					undoStack.Push("New Image", fileName, saved, img, currColor, selection)
				}
			}
		case types.MenuResponseImageSave:
			response = types.MenuResponseNone
			if fileName == "(new)" || len(fileName) == 0 {
				go saveFileAs(prt, &fileName, img, &saved, currColor, selection, &undoStack)
			} else {
				go saveFile(prt, fileName, img, &saved, currColor, selection, &undoStack)
			}
		case types.MenuResponseImageOpen:
			response = types.MenuResponseNone
			go openFile(prt, &fileName, &img, &refreshSprites, &lastChannel, currColor, selection, &undoStack)
		case types.MenuResponseImageSaveAs:
			response = types.MenuResponseNone
			go saveFileAs(prt, &fileName, img, &saved, currColor, selection, &undoStack)
		case types.MenuResponseBulkConvertToDDS:
			response = types.MenuResponseNone
			taskIdx := len(backgroundTasks)
			backgroundTasks[types.TaskID(taskIdx)] = &types.BackgroundStatus{
				Name:     "Bulk DDS->EXR Conversion",
				Message:  "",
				Progress: 0,
				Total:    -1,
				Status:   types.TaskIdle,
			}
			go bulkConvertFiles(prt, true, backgroundTasks[types.TaskID(taskIdx)])
		case types.MenuResponseBulkConvertToEXR:
			response = types.MenuResponseNone
			taskIdx := len(backgroundTasks)
			backgroundTasks[types.TaskID(taskIdx)] = &types.BackgroundStatus{
				Name:     "Bulk EXR->DDS Conversion",
				Message:  "",
				Progress: 0,
				Total:    -1,
				Status:   types.TaskIdle,
			}
			go bulkConvertFiles(prt, false, backgroundTasks[types.TaskID(taskIdx)])
		case types.MenuResponseViewChannels:
			response = types.MenuResponseNone
			channelsVisible = !channelsVisible
		case types.MenuResponseViewColor:
			response = types.MenuResponseNone
			colorVisible = !colorVisible
		case types.MenuResponseViewGrid:
			response = types.MenuResponseNone
			gridVisible = !gridVisible
		case types.MenuResponseViewTools:
			response = types.MenuResponseNone
			toolsVisible = !toolsVisible
		case types.MenuResponseUndo:
			response = types.MenuResponseNone
			handleUndo(prt, &undoStack, index, &img, &refreshSprites, &lastChannel, &currColor, &selection)
		case types.MenuResponseRedo:
			response = types.MenuResponseNone
			handleRedo(prt, &undoStack, index, &img, &refreshSprites, &lastChannel, &currColor, &selection)
		default:
			// Do nothing
			response = types.MenuResponseNone
		}

		if gridVisible && sprite != nil {
			drawGrid(win, camZoom, sprite.Frame())
		}

		if (tool == toolSelect || tool == toolMoveSelected) && selection != pixel.ZR {
			drawSelection(win, camZoom, selection.Moved(selectionOffset))
		}

		if toolsVisible {
			tempPrevTool := tool
			drawToolWindow(&tool, &toolsVisible)
			if tool != tempPrevTool && tool == toolMoveSelected && selection != pixel.ZR {
				undoStack.Push("Start move pixels", fileName, saved, img, currColor, selection)
				handleStartMoveSelection(selection, sprite.Frame().Center(), img, &pasteImg, &refreshSprites, &prevTool, &tempPrevTool)
				saved = false
			}
			if tool != tempPrevTool && tempPrevTool == toolMoveSelected && selection != pixel.ZR {
				undoStack.Push("End move pixels", fileName, saved, img, currColor, selection)
				handleImageCombine(selection, sprite.Frame().Center(), img, pasteImg)
				pasteImg = nil
				refreshSprites = true
				saved = false
			}
		}

		if colorVisible {
			prevColor := currColor
			drawColorWindow(&precision, &currColor, &colorVisible)
			if prevColor != currColor {
				undoStack.DelayedPush(1*time.Second, "Edit Color", &fileName, &saved, &img, &currColor, &selection)
			}
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
		pixelSelection := pixel.Rect{
			Min: selection.Min.Add(center),
			Max: selection.Max.Add(center),
		}
		drawStatusBar(cam.Unproject(win.MousePosition()).Add(center), hovColor, backgroundTasks, pixelSelection)

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

			pasteGrayable, ok := getGrayable(pasteImg)
			if ok {
				pasteGrayable.SetGray(viewedChannel)
			}
			lastChannel = viewedChannel
			refreshSprites = true
		}
		win.SetTitle(fmt.Sprintf("%s - %s%s", baseTitle, fileName, modified))

		camZoom *= math.Pow(camZoomSpeed, ui.MouseScroll().Y)

		win.Update()
	}
}

func handleUndo(prt *app.Printer, undoStack *types.UndoRedoStack, index int, img *image.Image, refreshSprite *bool, lastChannel *hdrColors.GraySetting, currColor *[4]float32, selection *pixel.Rect) {
	state, err := undoStack.Undo(index)
	if err != nil {
		prt.Errorf("%v", err)
		return
	}
	restoreState(prt, state, img, refreshSprite, lastChannel, currColor, selection)
}

func handleRedo(prt *app.Printer, undoStack *types.UndoRedoStack, index int, img *image.Image, refreshSprite *bool, lastChannel *hdrColors.GraySetting, currColor *[4]float32, selection *pixel.Rect) {
	state, err := undoStack.Redo(index)
	if err != nil {
		prt.Errorf("%v", err)
		return
	}
	restoreState(prt, state, img, refreshSprite, lastChannel, currColor, selection)
}

func restoreState(prt *app.Printer, state *types.UndoRedoState, img *image.Image, refreshSprite *bool, lastChannel *hdrColors.GraySetting, currColor *[4]float32, selection *pixel.Rect) {
	if len(state.Img) > 0 {
		bufR := bufio.NewReader(bytes.NewBuffer(state.Img))
		exr, err := openexr.LoadOpenEXR(*bufR)
		if err != nil {
			prt.Errorf("%v", err)
			return
		}
		newImg, err := exr.HdrImage()
		if err != nil {
			prt.Errorf("%v", err)
			return
		}
		*img = newImg
	}
	*refreshSprite = true
	*lastChannel = hdrColors.GraySettingAlpha
	*currColor = state.Color
	*selection = state.Selection
}

func writeImage(out io.Writer, img image.Image, fileName string) (err error) {
	if filepath.Ext(fileName) == ".exr" {
		err = openexr.WriteHDR(out, img)
	} else if filepath.Ext(fileName) == ".dds" {
		err = dds.WriteHDR(out, img)
	} else {
		err = fmt.Errorf("only saving to .exr or .dds implemented currently")
	}
	return
}

func saveFile(prt *app.Printer, fileName string, img image.Image, saved *bool, currColor [4]float32, selection pixel.Rect, undoStack *types.UndoRedoStack) {
	out, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		prt.Errorf("failed to save: %v", err)
		return
	}

	defer out.Close()

	err = writeImage(out, img, fileName)
	if err != nil {
		prt.Errorf("failed to write img to %s: %v", fileName, err)
		return
	}
	length, err := out.Seek(0, io.SeekCurrent)
	if err == nil {
		out.Truncate(length)
	}
	*saved = true
	undoStack.Push("Save File", fileName, true, img, currColor, selection)
}

func saveFileAs(prt *app.Printer, fileName *string, img image.Image, saved *bool, currColor [4]float32, selection pixel.Rect, undoStack *types.UndoRedoStack) {
	nextFileName, err := dialog.File().Filter("DDS or EXR files", "dds", "exr").Save()
	if err == dialog.ErrCancelled {
		return
	} else if err != nil {
		prt.Errorf("%v", err)
		return
	}
	*fileName = nextFileName
	saveFile(prt, *fileName, img, saved, currColor, selection, undoStack)
}

func bulkConvertFiles(prt *app.Printer, exrToDDS bool, task *types.BackgroundStatus) {
	var directionString, globStr, outSuffix string
	if exrToDDS {
		directionString = "EXR to DDS"
		globStr = "*.exr"
		outSuffix = ".dds"
	} else {
		directionString = "DDS to EXR"
		globStr = "*.dds"
		outSuffix = ".exr"
	}
	cwd, err := os.Getwd()
	if err != nil {
		task.OnCancel()
		prt.Errorf("bulk convert: failed to get current working directory: %v", err)
		return
	}
	folderName, err := dialog.Directory().Title(fmt.Sprintf("Select folder to bulk convert %v...", directionString)).SetStartDir(cwd).Browse()
	if err == dialog.ErrCancelled {
		task.OnCancel()
		return
	} else if err != nil {
		prt.Errorf("bulk convert: failed to get directory: %v", err)
		task.OnCancel()
		return
	}

	var success, failed int = 0, 0
	matches, err := filepath.Glob(filepath.Join(folderName, globStr))
	for idx, path := range matches {
		convImg, err := loadImage(path)
		if task != nil && err != nil {
			task.OnProgress(idx+1, len(matches), err)
		}
		if err != nil {
			prt.Errorf("bulk convert: failed to load %v: %v", path, err)
			failed += 1
			continue
		}

		convertedPath := strings.TrimSuffix(path, filepath.Ext(path)) + outSuffix
		out, err := os.OpenFile(convertedPath, os.O_CREATE|os.O_WRONLY, 0644)
		if task != nil && err != nil {
			task.OnProgress(idx+1, len(matches), err)
		}
		if err != nil {
			prt.Errorf("bulk convert: failed to open %v: %v", convertedPath, err)
			failed += 1
			continue
		}
		defer out.Close()

		err = writeImage(out, convImg, convertedPath)
		if err != nil {
			prt.Errorf("bulk convert: failed to write %v: %v", convertedPath, err)
			failed += 1
		} else {
			success += 1
		}
		if task != nil {
			task.OnProgress(idx+1, len(matches), err)
		}
	}
	if task != nil {
		task.OnComplete(success, failed, len(matches))
	}
}

func openFile(prt *app.Printer, fileName *string, img *image.Image, refreshSprite *bool, lastChannel *hdrColors.GraySetting, currColor [4]float32, selection pixel.Rect, undoStack *types.UndoRedoStack) {
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
	undoStack.Clear()
	undoStack.Push("Load File", *fileName, true, *img, currColor, selection)
}

func getPixelCoords(camera pixel.Matrix, spriteCenter pixel.Vec, mousePosition pixel.Vec) (x, y int) {
	coords := camera.Unproject(mousePosition).Add(spriteCenter)
	x, y = int(math.Floor(coords.X)), int(math.Floor(coords.Y))
	return
}

func fromPixelCoords(_ pixel.Matrix, spriteCenter pixel.Vec, x, y int) pixel.Vec {
	return pixel.V(float64(x), float64(y)).Sub(spriteCenter)
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

func drawToolWindow(currentTool *lmbTool, visible *bool) {
	imgui.BeginV("Tool", visible, imgui.WindowFlagsAlwaysAutoResize|imgui.WindowFlagsNoCollapse)
	{
		imgui.RadioButtonInt("Draw", (*int)(currentTool), int(toolDraw))
		imgui.RadioButtonInt("Select", (*int)(currentTool), int(toolSelect))
		imgui.RadioButtonInt("Move Selected Pixels", (*int)(currentTool), int(toolMoveSelected))
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

func drawSelection(win *opengl.Window, camZoom float64, selectionArea pixel.Rect) {
	selectionBox := imdraw.New(nil)
	selectionColor := pixel.RGBA{
		R: 0.4,
		G: 0.4,
		B: 0.7,
		A: 0.25,
	}

	lineWidth := 1.0 / camZoom
	selectionBox.Color = selectionColor
	selectionBox.Push(selectionArea.Min)
	selectionBox.Push(selectionArea.Max)
	selectionBox.Rectangle(lineWidth)
	selectionColor.A = 0.0625
	selectionBox.Color = selectionColor
	for x := selectionArea.Min.X; x < selectionArea.Max.X; x += 1 {
		for y := selectionArea.Min.Y; y < selectionArea.Max.Y; y += 1 {
			selectionBox.Push(pixel.V(x, y))
			selectionBox.Push(pixel.V(x+1, y+1))
			selectionBox.Line(lineWidth)
		}
	}
	selectionBox.Draw(win)
}

func drawStatusBar(mousePos pixel.Vec, color [4]float32, tasks types.TaskMap, selection pixel.Rect) {
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
			imgui.Textf("Mouse: (%.1f, %.1f) RGBA: (%3.3f, %3.3f, %3.3f, %3.3f)", mousePos.X, mousePos.Y, color[0], color[1], color[2], color[3])
			imgui.Separator()
			imgui.Textf("Selection: (%d, %d) -> (%d, %d)", int(selection.Min.X), int(selection.Min.Y), int(selection.Max.X), int(selection.Max.Y))
			imgui.Separator()
			var lastTask types.TaskID = types.TaskID(0xFFFFFFFF)
			imgui.BeginGroup()
			for idx, task := range tasks {
				lastTask = idx
				if task.Status == types.TaskRunning {
					break
				}
			}
			task, ok := tasks[lastTask]
			if ok {
				switch task.Status {
				case types.TaskRunning:
					imgui.Text(task.Name)
					imgui.ProgressBar(float32(task.Progress) / float32(task.Total))
				case types.TaskIdle:
					imgui.Text(task.Name)
					imgui.ProgressBarV(-float32(imgui.Time()), imgui.Vec2{X: -1.0, Y: 0.0}, "Starting...")
				case types.TaskFinished, types.TaskFailed:
					imgui.Textf("%v: %v", task.Name, task.Message)
				case types.TaskCancelled:
					delete(tasks, lastTask)
				}
			}
			imgui.EndGroup()
			imgui.EndMenuBar()
		}
		imgui.End()
	}
}

func createNewImage(img *image.Image, newImageConfirm, refreshSprite, saved *bool, fileName *string, lastChannel *hdrColors.GraySetting, width, height *int32, precision *int) bool {
	viewport := imgui.MainViewport()
	windowSize := imgui.Vec2{
		X: 0.2 * viewport.Size().X,
		Y: 0.2 * viewport.Size().Y,
	}
	var responded bool
	centerWindow(windowSize)
	if *newImageConfirm || confirmationDialog(windowSize, "Create new file?", "New File", "Confirm", "Cancel", &responded) {
		if newImageDialog(width, height, precision, windowSize, &responded) {
			*width = max(*width, 1)
			*height = max(*height, 1)
			switch *precision {
			case 0:
				*img = hdrColors.NewNRGBA128FImage(image.Rect(0, 0, int(*width), int(*height)))
			case 1:
				*img = hdrColors.NewNRGBA64FImage(image.Rect(0, 0, int(*width), int(*height)))
			}
			*refreshSprite = true
			*saved = false
			*fileName = "(new)"
			*lastChannel = hdrColors.GraySettingNone
		}
		*newImageConfirm = !responded || *img == nil
	}
	return responded
}

func getGrayable(img image.Image) (hdrColors.Grayable, bool) {
	if img == nil {
		return nil, false
	}
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

func selectionToImageRect(selection pixel.Rect, center pixel.Vec, height int) image.Rectangle {
	pixelSelection := pixel.Rect{
		Min: selection.Min.Add(center),
		Max: selection.Max.Add(center),
	}
	return image.Rect(
		int(pixelSelection.Min.X),
		height-int(pixelSelection.Min.Y),
		int(pixelSelection.Max.X),
		height-int(pixelSelection.Max.Y),
	)
}

func imageToSelectionRect(rect image.Rectangle, center pixel.Vec, height int) pixel.Rect {
	pixelSelection := pixel.Rect{
		Min: pixel.V(float64(rect.Min.X), float64(height-rect.Min.Y)),
		Max: pixel.V(float64(rect.Max.X), float64(height-rect.Max.Y)),
	}
	return pixelSelection.Moved(center.Scaled(-1))
}

func handleCopy(selection pixel.Rect, center pixel.Vec, img image.Image) error {
	imageRect := selectionToImageRect(selection, center, img.Bounds().Dy())
	copiedImg := copySubImage(img, imageRect)
	err := clipboard.WriteHDR(copiedImg)
	if err != nil {
		return err
	}
	return clipboard.WriteRect(selection)
}

func handleCut(selection pixel.Rect, center pixel.Vec, img image.Image) error {
	imageRect := selectionToImageRect(selection, center, img.Bounds().Dy())
	cutImg := cutSubImage(img, imageRect)
	err := clipboard.WriteHDR(cutImg)
	if err != nil {
		return err
	}
	return clipboard.WriteRect(selection)
}

func handleStartMoveSelection(selection pixel.Rect, center pixel.Vec, img image.Image, pasteImg *image.Image, refreshSprites *bool, prevTool, tool *lmbTool) {
	imageRect := selectionToImageRect(selection, center, img.Bounds().Dy())
	*pasteImg = cutSubImage(img, imageRect)
	*refreshSprites = true
	*prevTool = *tool
	*tool = toolMoveSelected
}

func handleImageCombine(selection pixel.Rect, center pixel.Vec, img image.Image, pasteImg image.Image) {
	imageRect := selectionToImageRect(selection, center, img.Bounds().Dy())
	combineSubImage(img, pasteImg, imageRect)
}

func copySubImage(img image.Image, selection image.Rectangle) image.Image {
	switch img.ColorModel() {
	case hdrColors.NRGBA128FModel:
		hdr, ok := img.(*hdrColors.NRGBA128FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA128FImage)
			if !ok {
				break
			}
		}
		oldGray := hdr.Grayscale
		hdr.SetGray(hdrColors.GraySettingNone)
		toReturn := hdrColors.NewNRGBA128FImage(image.Rect(0, 0, selection.Dx(), selection.Dy()))
		for y := selection.Min.Y; y < selection.Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				toReturn.Set(retX, retY, hdr.NRGBA128FAt(x, y))
			}
		}
		toReturn.SetGray(oldGray)
		hdr.SetGray(oldGray)
		return toReturn
	case hdrColors.NRGBA128UModel:
		hdr, ok := img.(*hdrColors.NRGBA128UImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA128UImage)
			if !ok {
				break
			}
		}
		oldGray := hdr.Grayscale
		hdr.SetGray(hdrColors.GraySettingNone)
		toReturn := hdrColors.NewNRGBA128UImage(image.Rect(0, 0, selection.Dx(), selection.Dy()))
		for y := selection.Min.Y; y < selection.Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				toReturn.Set(retX, retY, hdr.NRGBA128UAt(x, y))
			}
		}
		toReturn.SetGray(oldGray)
		hdr.SetGray(oldGray)
		return toReturn
	case hdrColors.NRGBA64FModel:
		hdr, ok := img.(*hdrColors.NRGBA64FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA64FImage)
			if !ok {
				break
			}
		}
		oldGray := hdr.Grayscale
		hdr.SetGray(hdrColors.GraySettingNone)
		toReturn := hdrColors.NewNRGBA64FImage(image.Rect(0, 0, selection.Dx(), selection.Dy()))
		for y := selection.Min.Y; y < selection.Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				toReturn.Set(retX, retY, hdr.NRGBA64FAt(x, y))
			}
		}
		toReturn.SetGray(oldGray)
		hdr.SetGray(oldGray)
		return toReturn
	}
	return nil
}

func cutSubImage(img image.Image, selection image.Rectangle) image.Image {
	switch img.ColorModel() {
	case hdrColors.NRGBA128FModel:
		hdr, ok := img.(*hdrColors.NRGBA128FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA128FImage)
			if !ok {
				break
			}
		}
		oldGray := hdr.Grayscale
		hdr.SetGray(hdrColors.GraySettingNone)
		toReturn := hdrColors.NewNRGBA128FImage(image.Rect(0, 0, selection.Dx(), selection.Dy()))
		for y := selection.Min.Y; y < selection.Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				toReturn.Set(retX, retY, hdr.NRGBA128FAt(x, y))
				hdr.Set(x, y, hdrColors.NRGBA128F{
					R: 0,
					G: 0,
					B: 0,
					A: 0,
				})
			}
		}
		toReturn.SetGray(oldGray)
		hdr.SetGray(oldGray)
		return toReturn
	case hdrColors.NRGBA128UModel:
		hdr, ok := img.(*hdrColors.NRGBA128UImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA128UImage)
			if !ok {
				break
			}
		}
		oldGray := hdr.Grayscale
		hdr.SetGray(hdrColors.GraySettingNone)
		toReturn := hdrColors.NewNRGBA128UImage(image.Rect(0, 0, selection.Dx(), selection.Dy()))
		for y := selection.Min.Y; y < selection.Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				toReturn.Set(retX, retY, hdr.NRGBA128UAt(x, y))
				hdr.Set(x, y, hdrColors.NRGBA128U{
					R: 0,
					G: 0,
					B: 0,
					A: 0,
				})
			}
		}
		toReturn.SetGray(oldGray)
		hdr.SetGray(oldGray)
		return toReturn
	case hdrColors.NRGBA64FModel:
		hdr, ok := img.(*hdrColors.NRGBA64FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA64FImage)
			if !ok {
				break
			}
		}
		oldGray := hdr.Grayscale
		hdr.SetGray(hdrColors.GraySettingNone)
		toReturn := hdrColors.NewNRGBA64FImage(image.Rect(0, 0, selection.Dx(), selection.Dy()))
		for y := selection.Min.Y; y < selection.Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				toReturn.Set(retX, retY, hdr.NRGBA64FAt(x, y))
				hdr.Set(x, y, hdrColors.NRGBA64F{
					R: 0,
					G: 0,
					B: 0,
					A: 0,
				})
			}
		}
		toReturn.SetGray(oldGray)
		hdr.SetGray(oldGray)
		return toReturn
	}
	return nil
}

func combineSubImage(img, pasteImg image.Image, selection image.Rectangle) {
	switch img.ColorModel() {
	case hdrColors.NRGBA128FModel:
		hdr, ok := img.(*hdrColors.NRGBA128FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA128FImage)
			if !ok {
				break
			}
		}
		pasteHdr, ok := pasteImg.(*hdrColors.NRGBA128FImage)
		if !ok {
			break
		}
		oldGray := pasteHdr.Grayscale
		pasteHdr.SetGray(hdrColors.GraySettingNone)
		for y := int(math.Max(0, float64(selection.Min.Y))); y < selection.Max.Y && y < img.Bounds().Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				hdr.Set(x, y, pasteHdr.NRGBA128FAt(retX, retY))
			}
		}
		pasteHdr.SetGray(oldGray)
	case hdrColors.NRGBA128UModel:
		hdr, ok := img.(*hdrColors.NRGBA128UImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA128UImage)
			if !ok {
				break
			}
		}
		pasteHdr, ok := pasteImg.(*hdrColors.NRGBA128UImage)
		if !ok {
			break
		}
		oldGray := pasteHdr.Grayscale
		pasteHdr.SetGray(hdrColors.GraySettingNone)
		for y := int(math.Max(0, float64(selection.Min.Y))); y < selection.Max.Y && y < img.Bounds().Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				hdr.Set(x, y, pasteHdr.NRGBA128UAt(retX, retY))
			}
		}
		pasteHdr.SetGray(oldGray)
	case hdrColors.NRGBA64FModel:
		hdr, ok := img.(*hdrColors.NRGBA64FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				break
			}
			hdr, ok = ddsImg.Image.(*hdrColors.NRGBA64FImage)
			if !ok {
				break
			}
		}
		pasteHdr, ok := pasteImg.(*hdrColors.NRGBA64FImage)
		if !ok {
			break
		}
		oldGray := pasteHdr.Grayscale
		pasteHdr.SetGray(hdrColors.GraySettingNone)
		for y := int(math.Max(0, float64(selection.Min.Y))); y < selection.Max.Y && y < img.Bounds().Max.Y; y++ {
			retY := y - selection.Min.Y
			for x := selection.Min.X; x < selection.Max.X; x++ {
				retX := x - selection.Min.X
				hdr.Set(x, y, pasteHdr.NRGBA64FAt(retX, retY))
			}
		}
		pasteHdr.SetGray(oldGray)
	}
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

func centerWindow(windowSize imgui.Vec2) {
	viewport := imgui.MainViewport()
	imgui.SetNextWindowPos(imgui.Vec2{
		X: 0.5 * (viewport.Size().X - windowSize.X),
		Y: 0.5 * (viewport.Size().Y - windowSize.Y),
	})
	imgui.SetNextWindowSize(windowSize)
}

func confirmationDialog(windowSize imgui.Vec2, text, title, confirm, deny string, responded *bool) (response bool) {
	*responded = false
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
		*responded = true
		response = true
	}
	imgui.SameLine()
	imgui.SetCursorPos(imgui.Vec2{
		X: windowSize.X * 0.65,
		Y: imgui.CursorPosY(),
	})
	if imgui.ButtonV(deny, buttonSize) {
		*responded = true
		response = false
	}
	imgui.End()
	return
}

func newImageDialog(width, height *int32, precision *int, windowSize imgui.Vec2, responded *bool) (resp bool) {
	*responded = false
	imgui.BeginV("New file settings", nil, imgui.WindowFlagsNoMove|imgui.WindowFlagsNoResize|imgui.WindowFlagsNoCollapse)
	imgui.InputInt("Width", width)
	imgui.InputInt("Height", height)
	imgui.RadioButtonInt("Float", precision, 0)
	imgui.SameLine()
	imgui.RadioButtonInt("Half", precision, 1)
	buttonSize := imgui.Vec2{
		X: windowSize.X * 0.3,
		Y: windowSize.Y * 0.2,
	}
	imgui.SetCursorPos(imgui.Vec2{
		X: imgui.CursorPosX(),
		Y: windowSize.Y * .75,
	})
	if imgui.ButtonV("OK", buttonSize) {
		*responded = true
		resp = true
	}
	imgui.SameLine()
	imgui.SetCursorPos(imgui.Vec2{
		X: windowSize.X * 0.65,
		Y: imgui.CursorPosY(),
	})
	if imgui.ButtonV("Cancel", buttonSize) {
		*responded = true
		resp = false
	}
	imgui.End()
	return
}

func showMainMenuBar(img image.Image, channelsVisible, colorVisible, gridVisible, toolsVisible bool, undoStack *types.UndoRedoStack) (types.MenuResponse, int) {
	response := types.MenuResponseNone
	index := -1
	if imgui.BeginMainMenuBar() {
		if imgui.BeginMenu("File") {
			response = showFileMenu(img)
			imgui.EndMenu()
		}
		if imgui.BeginMenu("Edit") {
			response, index = showEditMenu(undoStack)
			imgui.EndMenu()
		}
		if imgui.BeginMenu("View") {
			response = showViewMenu(channelsVisible, colorVisible, gridVisible, toolsVisible)
			imgui.EndMenu()
		}
		imgui.EndMainMenuBar()
	}
	return response, index
}

func showFileMenu(img image.Image) types.MenuResponse {
	response := types.MenuResponseNone
	if imgui.MenuItem("New") {
		response = types.MenuResponseImageNew
	}
	if imgui.MenuItem("Open...") {
		response = types.MenuResponseImageOpen
	}
	if imgui.MenuItemV("Save", "", false, img != nil) {
		response = types.MenuResponseImageSave
	}
	if imgui.MenuItemV("Save As...", "", false, img != nil) {
		response = types.MenuResponseImageSaveAs
	}
	if imgui.MenuItem("Convert to DDS...") {
		response = types.MenuResponseBulkConvertToDDS
	}
	if imgui.MenuItem("Convert to EXR...") {
		response = types.MenuResponseBulkConvertToEXR
	}
	return response
}

func showEditMenu(undoStack *types.UndoRedoStack) (resp types.MenuResponse, index int) {
	if imgui.MenuItemV("Undo", "ctrl-z", false, len(undoStack.UndoStack) > 0) {
		resp = types.MenuResponseUndo
		index = max(len(undoStack.UndoStack)-2, 0)
	}
	if imgui.BeginMenuV("Undo...", len(undoStack.UndoStack) > 0) {
		for i, undoItem := range undoStack.UndoStack {
			if imgui.MenuItem(undoItem.Action) {
				resp = types.MenuResponseUndo
				index = i
			}
		}
		imgui.EndMenu()
	}
	if imgui.MenuItemV("Redo", "ctrl-shift-z", false, len(undoStack.RedoStack) > 0) {
		resp = types.MenuResponseRedo
		index = max(len(undoStack.RedoStack)-1, 0)
	}
	if imgui.BeginMenuV("Redo...", len(undoStack.RedoStack) > 0) {
		for i, redoItem := range undoStack.RedoStack {
			if imgui.MenuItem(redoItem.Action) {
				resp = types.MenuResponseRedo
				index = i
			}
		}
		imgui.EndMenu()
	}
	return
}

func showViewMenu(channelsVisible, colorVisible, gridVisible, toolsVisible bool) types.MenuResponse {
	response := types.MenuResponseNone
	if imgui.MenuItemV("Channels", "", channelsVisible, true) {
		response = types.MenuResponseViewChannels
	}
	if imgui.MenuItemV("Color", "", colorVisible, true) {
		response = types.MenuResponseViewColor
	}
	if imgui.MenuItemV("Grid", "", gridVisible, true) {
		response = types.MenuResponseViewGrid
	}
	if imgui.MenuItemV("Tools", "", toolsVisible, true) {
		response = types.MenuResponseViewTools
	}
	return response
}

func main() {
	opengl.Run(run)
}
