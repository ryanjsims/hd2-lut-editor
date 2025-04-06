# HD2 LUT Editor

This is a laser-focused, bare-bones image editor that can edit DDS and OpenEXR 16- and 32-bit floating point images. Intended for use with Helldivers 2 material look up tables, alongside the [HD2 LUT Modding Guide](https://docs.google.com/document/d/1A_bsjh-wc6nhuxYodRk5LURugD0mQd2kyctkOqZysks/edit?tab=t.0#heading=h.2lwd1wk1xu40).

This is **not** meant to be a general purpose image editor, or even a particularly great image editor.

## Download

Latest release is available [here](https://github.com/ryanjsims/hd2-lut-editor/releases/latest), and has a prebuilt exe for Windows under Assets

## Usage

You can open a DDS or OpenEXR image via the File menu, or if you run the editor from the command-line you may provide a path to an image to open.

Left click sets the hovered pixel to the current color, and right click selects the hovered pixel's color.

If the program crashes, there should be a message about what 

## Building
### Windows
`go build -ldflags '-extldflags "-static" -H windowsgui' .\cmd\lut-editor\`

## Future Plans (with no particular ETA)

* Undo/Redo stack
* Copy/Paste areas of pixels
* Help window integrating info from the guide
