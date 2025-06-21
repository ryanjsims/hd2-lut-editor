# HD2 LUT Editor

This is a laser-focused, bare-bones image editor that can edit DDS and OpenEXR 16- and 32-bit floating point images. Intended for use with Helldivers 2 material look up tables, alongside the [HD2 LUT Modding Guide](https://docs.google.com/document/d/1A_bsjh-wc6nhuxYodRk5LURugD0mQd2kyctkOqZysks/edit?tab=t.0#heading=h.2lwd1wk1xu40).

This is **not** meant to be a general purpose image editor, or even a particularly great image editor.

## Download

Latest release is available [here](https://github.com/ryanjsims/hd2-lut-editor/releases/latest), and has a prebuilt exe for Windows under Assets

## Usage

You can open a DDS or OpenEXR image via the File menu, or if you run the editor from the command-line you may provide a path to an image to open. You can also drag a DDS/EXR file onto the executable to open it.

4 tools are available:
1. Draw - left click to place the current color at the current pixel
2. Select - left click and drag to select an area of pixels
3. Move selected pixels - left click and drag to move the currently selected pixels
4. Pick color - right click on a pixel to make its color the current color

Several view modes are available, to preview the different channels of an image:
* RGB
* RGBA
* Red
* Green
* Blue
* Alpha

There are also several shortcuts which should be fairly standard for image editors:
* Ctrl-N: create a new file
* Ctrl-O: open an existing file
* Ctrl-S: save the current file
* Ctrl-Shift-S: save the current file with a new name
* Ctrl-C: copy the currently selected pixels to the clipboard
* Ctrl-X: cut the currently selected pixels to the clipboard
* Ctrl-V: paste the pixels from the clipboard and enter move selected pixels mode
* Enter: finish moving pixels and apply the changes
* Ctrl-Z: Undo previous action
* Ctrl-Shift-Z: Redo previously undone action

If the program crashes, there should be a message about what happened in `lut-editor.log` located in the same directory as `lut-editor.exe`.

## Building
### Windows
`go build -ldflags '-extldflags "-static" -H windowsgui' .\cmd\lut-editor\`

## Future Plans (with no particular ETA)
* Help window integrating info from the guide
