package clipboard

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"runtime"
	"syscall"
	"unsafe"

	"github.com/gopxl/pixel/v2"
	"github.com/ryanjsims/hd2-lut-editor/hdrColors"
)

// Calling a Windows DLL, see:
// https://go.dev/wiki/WindowsDLLs
var (
	user32 = syscall.MustLoadDLL("user32")
	// Opens the clipboard for examination and prevents other
	// applications from modifying the clipboard content.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-openclipboard
	openClipboard = user32.MustFindProc("OpenClipboard")
	// Closes the clipboard.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-closeclipboard
	closeClipboard = user32.MustFindProc("CloseClipboard")
	// Empties the clipboard and frees handles to data in the clipboard.
	// The function then assigns ownership of the clipboard to the
	// window that currently has the clipboard open.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-emptyclipboard
	emptyClipboard = user32.MustFindProc("EmptyClipboard")
	// Retrieves data from the clipboard in a specified format.
	// The clipboard must have been opened previously.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclipboarddata
	getClipboardData = user32.MustFindProc("GetClipboardData")
	// Places data on the clipboard in a specified clipboard format.
	// The window must be the current clipboard owner, and the
	// application must have called the OpenClipboard function. (When
	// responding to the WM_RENDERFORMAT message, the clipboard owner
	// must not call OpenClipboard before calling SetClipboardData.)
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-setclipboarddata
	setClipboardData = user32.MustFindProc("SetClipboardData")
	// Determines whether the clipboard contains data in the specified format.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-isclipboardformatavailable
	isClipboardFormatAvailable = user32.MustFindProc("IsClipboardFormatAvailable")
	// Clipboard data formats are stored in an ordered list. To perform
	// an enumeration of clipboard data formats, you make a series of
	// calls to the EnumClipboardFormats function. For each call, the
	// format parameter specifies an available clipboard format, and the
	// function returns the next available clipboard format.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-isclipboardformatavailable
	enumClipboardFormats = user32.MustFindProc("EnumClipboardFormats")
	// Retrieves the clipboard sequence number for the current window station.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-getclipboardsequencenumber
	getClipboardSequenceNumber = user32.MustFindProc("GetClipboardSequenceNumber")
	// Registers a new clipboard format. This format can then be used as
	// a valid clipboard format.
	// https://docs.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-registerclipboardformata
	registerClipboardFormatA = user32.MustFindProc("RegisterClipboardFormatA")

	kernel32 = syscall.NewLazyDLL("kernel32")

	// Locks a global memory object and returns a pointer to the first
	// byte of the object's memory block.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globallock
	gLock = kernel32.NewProc("GlobalLock")
	// Decrements the lock count associated with a memory object that was
	// allocated with GMEM_MOVEABLE. This function has no effect on memory
	// objects allocated with GMEM_FIXED.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalunlock
	gUnlock = kernel32.NewProc("GlobalUnlock")
	// Allocates the specified number of bytes from the heap.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalalloc
	gAlloc = kernel32.NewProc("GlobalAlloc")
	// Frees the specified global memory object and invalidates its handle.
	// https://docs.microsoft.com/en-us/windows/win32/api/winbase/nf-winbase-globalfree
	gFree   = kernel32.NewProc("GlobalFree")
	memMove = kernel32.NewProc("RtlMoveMemory")
)

var (
	formatHDR  uint32
	formatRect uint32
)

const (
	imgFormatFloat16 uint32 = iota
	imgFormatFloat32 uint32 = iota
	imgFormatUInt32  uint32 = iota
)

const (
	gMemMoveable = 0x0002
)

var (
	ErrUnavailable = errors.New("clipboard unavailable")
)

type HDRHeader struct {
	format     uint32
	bounds     image.Rectangle
	stride     int
	byteLength int
}

func Init() error {
	formatNameHDR, err := syscall.BytePtrFromString("helldivers lut editor hdr image")
	if err != nil {
		return fmt.Errorf("failed to convert string to byte ptr")
	}
	pFmtName := unsafe.Pointer(formatNameHDR)
	r, _, err := registerClipboardFormatA.Call(uintptr(pFmtName))

	if r == 0 {
		return err
	}
	formatHDR = uint32(r)

	formatNameRect, err := syscall.BytePtrFromString("helldivers lut editor rectangle")
	if err != nil {
		return fmt.Errorf("failed to convert string to byte ptr")
	}
	pFmtName = unsafe.Pointer(formatNameRect)
	r, _, err = registerClipboardFormatA.Call(uintptr(pFmtName))

	if r == 0 {
		return err
	}
	formatRect = uint32(r)
	return nil
}

func WriteHDR(img image.Image) error {
	switch img.ColorModel() {
	case hdrColors.NRGBA128FModel, hdrColors.NRGBA128UModel, hdrColors.NRGBA64FModel:
		break
	default:
		return fmt.Errorf("not an HDR image")
	}

	hdrImg, ok := img.(hdrColors.HDRImage)
	if !ok {
		return fmt.Errorf("not an HDR image")
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for {
		r, _, _ := openClipboard.Call(0)
		if r == 0 {
			continue
		}
		break
	}
	defer closeClipboard.Call()

	var buf bytes.Buffer
	var header HDRHeader

	switch img.ColorModel() {
	case hdrColors.NRGBA64FModel:
		header.format = imgFormatFloat16
	case hdrColors.NRGBA128FModel:
		header.format = imgFormatFloat32
	case hdrColors.NRGBA128UModel:
		header.format = imgFormatUInt32
	}
	header.bounds = img.Bounds()
	header.stride = hdrImg.GetStride()
	header.byteLength = len(hdrImg.Pixels())
	binary.Write(&buf, binary.LittleEndian, header)
	binary.Write(&buf, binary.LittleEndian, hdrImg.Pixels())

	hMem, _, err := gAlloc.Call(gMemMoveable, uintptr(buf.Len()))
	if hMem == 0 {
		return fmt.Errorf("failed to alloc global memory: %w", err)
	}

	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return fmt.Errorf("failed to lock global memory: %w", err)
	}
	defer gUnlock.Call(hMem)

	imgDataPtr := unsafe.Pointer(&buf.Bytes()[0])
	memMove.Call(p, uintptr(imgDataPtr), uintptr(buf.Len()))

	v, _, err := setClipboardData.Call(uintptr(formatHDR), hMem)
	if v == 0 {
		gFree.Call(hMem)
		return fmt.Errorf("failed to write HDR image to clipboard: %w", err)
	}

	return nil
}

func WriteRect(rect pixel.Rect) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	for {
		r, _, _ := openClipboard.Call(0)
		if r == 0 {
			continue
		}
		break
	}
	defer closeClipboard.Call()

	var buf bytes.Buffer

	binary.Write(&buf, binary.LittleEndian, rect)

	hMem, _, err := gAlloc.Call(gMemMoveable, uintptr(buf.Len()))
	if hMem == 0 {
		return fmt.Errorf("failed to alloc global memory: %w", err)
	}

	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return fmt.Errorf("failed to lock global memory: %w", err)
	}
	defer gUnlock.Call(hMem)

	rectDataPtr := unsafe.Pointer(&buf.Bytes()[0])
	memMove.Call(p, uintptr(rectDataPtr), uintptr(buf.Len()))

	v, _, err := setClipboardData.Call(uintptr(formatRect), hMem)
	if v == 0 {
		gFree.Call(hMem)
		return fmt.Errorf("failed to write rectangle to clipboard: %w", err)
	}

	return nil
}

func ReadHDR() (image.Image, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, _ := isClipboardFormatAvailable.Call(uintptr(formatHDR))
	if r == 0 {
		return nil, ErrUnavailable
	}

	for {
		r, _, _ := openClipboard.Call(0)
		if r == 0 {
			continue
		}
		break
	}
	defer closeClipboard.Call()

	hMem, _, err := getClipboardData.Call(uintptr(formatHDR))
	if hMem == 0 {
		return nil, err
	}
	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return nil, err
	}
	defer gUnlock.Call(hMem)

	ptr := unsafe.Pointer(p)
	data := (*HDRHeader)(ptr)
	pxPtr := unsafe.Pointer(p + unsafe.Sizeof(data))
	pixels := unsafe.Slice((*uint8)(pxPtr), data.byteLength)

	var img image.Image
	switch data.format {
	case imgFormatFloat16:
		hdrImg := hdrColors.NewNRGBA64FImage(data.bounds)
		copy(hdrImg.Pix, pixels)
		img = hdrImg
	case imgFormatFloat32:
		hdrImg := hdrColors.NewNRGBA128FImage(data.bounds)
		copy(hdrImg.Pix, pixels)
		img = hdrImg
	case imgFormatUInt32:
		hdrImg := hdrColors.NewNRGBA128UImage(data.bounds)
		copy(hdrImg.Pix, pixels)
		img = hdrImg
	}
	return img, nil
}

func ReadRect() (*pixel.Rect, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	r, _, _ := isClipboardFormatAvailable.Call(uintptr(formatHDR))
	if r == 0 {
		return nil, ErrUnavailable
	}

	for {
		r, _, _ := openClipboard.Call(0)
		if r == 0 {
			continue
		}
		break
	}
	defer closeClipboard.Call()

	hMem, _, err := getClipboardData.Call(uintptr(formatHDR))
	if hMem == 0 {
		return nil, err
	}
	p, _, err := gLock.Call(hMem)
	if p == 0 {
		return nil, err
	}
	defer gUnlock.Call(hMem)

	ptr := unsafe.Pointer(p)
	data := (*pixel.Rect)(ptr)

	// Copy the data from the global memory
	toReturn := *data

	return &toReturn, nil
}
