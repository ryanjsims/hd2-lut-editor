package types

import (
	"bytes"
	"fmt"
	"image"
	"slices"
	"time"

	"github.com/ryanjsims/hd2-lut-editor/openexr"
)

type UndoRedoState struct {
	Action   string
	filename string
	saved    bool
	Img      []byte
	Color    [4]float32
}

type UndoRedoStack struct {
	UndoStack []UndoRedoState
	RedoStack []UndoRedoState
	timer     *time.Timer
}

func (u *UndoRedoStack) Clear() {
	if u.timer != nil {
		u.timer.Stop()
		u.timer = nil
	}
	u.UndoStack = make([]UndoRedoState, 0)
	u.RedoStack = make([]UndoRedoState, 0)
}

func (u *UndoRedoStack) Push(action, filename string, saved bool, img image.Image, currColor [4]float32) {
	undoState := UndoRedoState{
		Action:   action,
		filename: filename,
		saved:    saved,
		Img:      make([]byte, 0),
		Color:    currColor,
	}
	if img != nil {
		buf := &bytes.Buffer{}
		openexr.WriteHDR(buf, img)
		undoState.Img = append(undoState.Img, buf.Bytes()...)
	}
	u.UndoStack = append(u.UndoStack, undoState)
}

func (u *UndoRedoStack) DelayedPush(d time.Duration, action string, filename *string, saved *bool, img *image.Image, currColor *[4]float32) {
	if u.timer != nil {
		u.timer.Stop()
	}
	u.timer = time.AfterFunc(d, func() { u.Push(action, *filename, *saved, *img, *currColor) })
}

func (u *UndoRedoStack) Undo(index int) (*UndoRedoState, error) {
	if !(index >= 0 && index < len(u.UndoStack)) {
		return nil, fmt.Errorf("Undo: invalid index")
	}
	slices.Reverse(u.UndoStack[index+1:])
	u.RedoStack = append(u.RedoStack, u.UndoStack[index+1:]...)
	toReturn := u.UndoStack[index]
	u.UndoStack = u.UndoStack[:index+1]
	return &toReturn, nil
}

func (u *UndoRedoStack) Redo(index int) (*UndoRedoState, error) {
	if !(index >= 0 && index < len(u.RedoStack)) {
		return nil, fmt.Errorf("Undo: invalid index")
	}
	toReturn := u.RedoStack[index]
	slices.Reverse(u.RedoStack[index:])
	u.UndoStack = append(u.UndoStack, u.RedoStack[index:]...)
	u.RedoStack = u.RedoStack[:index]
	return &toReturn, nil
}
