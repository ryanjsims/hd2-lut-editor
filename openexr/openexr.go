package openexr

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"io"
	"slices"

	"github.com/ryanjsims/hd2-lut-editor/dds"
	"github.com/ryanjsims/hd2-lut-editor/hdrColors"
	"github.com/x448/float16"
)

const EXR_MAGIC uint32 = 20000630

type PixelType uint32

const (
	TypeUInt  PixelType = 0
	TypeHalf  PixelType = 1
	TypeFloat PixelType = 2
)

func (t PixelType) String() string {
	switch t {
	case TypeUInt:
		return "uint32"
	case TypeHalf:
		return "float16"
	case TypeFloat:
		return "float32"
	default:
		return fmt.Sprint(uint32(t))
	}
}

func (t PixelType) Size() int {
	switch t {
	case TypeUInt:
		return 4
	case TypeHalf:
		return 2
	case TypeFloat:
		return 4
	default:
		return 0
	}
}

func (t PixelType) Model() color.Model {
	switch t {
	case TypeUInt:
		return hdrColors.NRGBA128UModel
	case TypeHalf:
		return hdrColors.NRGBA64FModel
	case TypeFloat:
		return hdrColors.NRGBA128FModel
	default:
		return nil
	}
}

type Compression uint8

const (
	CompressionNone  Compression = 0
	CompressionRLE   Compression = 1
	CompressionZIPS  Compression = 2
	CompressionZIP   Compression = 3
	CompressionPIZ   Compression = 4
	CompressionPXR24 Compression = 5
	CompressionB44   Compression = 6
	CompressionB44A  Compression = 7
	CompressionDWAA  Compression = 8
	CompressionDWAB  Compression = 9
)

func (c Compression) String() string {
	switch c {
	case CompressionNone:
		return "No compression"
	case CompressionRLE:
		return "Run length encoding"
	case CompressionZIPS:
		return "Zip (single scanline)"
	case CompressionZIP:
		return "Zip (multi scanline)"
	case CompressionPIZ:
		return "PIZ wavelet compression"
	case CompressionPXR24:
		return "Pixar 24 bit deflate"
	case CompressionB44:
		return "B44"
	case CompressionB44A:
		return "B44A"
	case CompressionDWAA:
		return "Dreamworks Animation 32 scanline"
	case CompressionDWAB:
		return "Dreamworks Animation 256 scanline"
	default:
		return fmt.Sprint(uint32(c))
	}
}

func (c Compression) LineCount() int {
	switch c {
	case CompressionNone:
		return 1
	case CompressionRLE:
		return 1
	case CompressionZIPS:
		return 1
	case CompressionZIP:
		return 16
	case CompressionPIZ:
		return 32
	case CompressionPXR24:
		return 16
	case CompressionB44:
		return 32
	case CompressionB44A:
		return 32
	case CompressionDWAA:
		return 32
	case CompressionDWAB:
		return 256
	default:
		return -1
	}
}

type LineOrder uint8

const (
	OrderIncreasingY LineOrder = 0
	OrderDecreasingY LineOrder = 1
	OrderRandomY     LineOrder = 2
)

func (l LineOrder) String() string {
	switch l {
	case OrderIncreasingY:
		return "increasing y"
	case OrderDecreasingY:
		return "decreasing y"
	case OrderRandomY:
		return "random y"
	default:
		return fmt.Sprint(uint8(l))
	}
}

type Box2i struct {
	XMin uint32
	YMin uint32
	XMax uint32
	YMax uint32
}

func (b Box2i) Width() uint32 {
	return b.XMax - b.XMin + 1
}

func (b Box2i) Height() uint32 {
	return b.YMax - b.YMin + 1
}

type Box2f struct {
	XMin float32
	YMin float32
	XMax float32
	YMax float32
}

func (b Box2f) Width() float32 {
	return b.XMax - b.XMin + 1
}

func (b Box2f) Height() float32 {
	return b.YMax - b.YMin + 1
}

type Channel struct {
	Name      string
	PixelFmt  PixelType
	Linear    uint32
	XSampling uint32
	YSampling uint32
}

type Attribute struct {
	Name string
	Type string
	Size uint32
	Data []uint8
}

type ScanLine struct {
	YCoord     uint32
	Size       uint32
	Data       []uint8
	Compressed bool
	LineCount  uint32
}

type OpenEXRHeader struct {
	Magic    uint32
	Version  uint8
	Flags    [3]uint8
	Channels []Channel
	Compression
	DataWindow    Box2i
	DisplayWindow Box2i
	LineOrder
	PixelAspectRatio   float32
	ScreenWindowCenter [2]float32
	ScreenWindowWidth  float32
	OffsetTable        []uint64
}

type OpenEXR struct {
	OpenEXRHeader
	ScanLines []ScanLine
}

func (s *ScanLine) offset(x, y, channel, depth int, typ PixelType, window Box2i) int64 {
	width := window.Width()

	bytesPerValue := typ.Size()

	if y >= int(s.YCoord+s.LineCount) || y < int(s.YCoord) || x < int(window.XMin) || x > int(window.XMax) {
		return -1
	}
	if s.Compressed {
		return -2
	}
	channelStride := width * uint32(bytesPerValue)
	lineStride := channelStride * uint32(depth)
	line := (uint32(y) - s.YCoord)
	return int64(line*lineStride + uint32(channel)*channelStride + uint32(x)*uint32(bytesPerValue))
}

func loadChannels(r *bufio.Reader) ([]Channel, error) {
	channels := make([]Channel, 0, 4)

	name, err := r.ReadString(0)
	for len(name) > 1 {
		if err != nil {
			return nil, err
		}

		var pixelFmt PixelType
		err = binary.Read(r, binary.LittleEndian, &pixelFmt)
		if err != nil {
			return nil, err
		}

		var pLinear uint32
		err = binary.Read(r, binary.LittleEndian, &pLinear)
		if err != nil {
			return nil, err
		}

		var xSampling uint32
		err = binary.Read(r, binary.LittleEndian, &xSampling)
		if err != nil {
			return nil, err
		}

		var ySampling uint32
		err = binary.Read(r, binary.LittleEndian, &ySampling)
		if err != nil {
			return nil, err
		}

		channels = append(channels, Channel{
			Name:      name[:len(name)-1],
			PixelFmt:  pixelFmt,
			Linear:    pLinear & 0xff,
			XSampling: xSampling,
			YSampling: ySampling,
		})

		name, err = r.ReadString(0)
	}

	if err != nil {
		return nil, err
	}

	return channels, nil
}

func loadEXRHeader(r *bufio.Reader) (*OpenEXRHeader, error) {
	var magic uint32
	if err := binary.Read(r, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}

	if magic != EXR_MAGIC {
		return nil, fmt.Errorf("invalid file magic %v", magic)
	}

	var version_flags [4]uint8
	if err := binary.Read(r, binary.LittleEndian, &version_flags); err != nil {
		return nil, err
	}

	if version_flags[0] != 2 {
		return nil, fmt.Errorf("unsupported EXR version %v", version_flags[0])
	}

	if version_flags[1] != 0 || version_flags[2] != 0 || version_flags[3] != 0 {
		return nil, fmt.Errorf("unsupported flags in EXR %02x %02x %02x", version_flags[1], version_flags[2], version_flags[3])
	}

	var (
		channels           []Channel
		compression        Compression
		dataWindow         Box2i
		displayWindow      Box2i
		lineOrder          LineOrder
		pixelAspectRatio   float32
		screenWindowCenter [2]float32
		screenWindowWidth  float32
	)

	var requiredFields []string = []string{
		"channels",
		"compression",
		"dataWindow",
		"displayWindow",
		"lineOrder",
		"pixelAspectRatio",
		"screenWindowCenter",
		"screenWindowWidth",
	}

	name, err := r.ReadString(0)
	for len(name) > 1 {
		if err != nil {
			return nil, err
		}

		_, err = r.ReadString(0)
		if err != nil {
			return nil, err
		}

		var size uint32
		err = binary.Read(r, binary.LittleEndian, &size)
		if err != nil {
			return nil, err
		}

		switch name[:len(name)-1] {
		case "channels":
			channels, err = loadChannels(r)
		case "compression":
			err = binary.Read(r, binary.LittleEndian, &compression)
		case "dataWindow":
			err = binary.Read(r, binary.LittleEndian, &dataWindow)
		case "displayWindow":
			err = binary.Read(r, binary.LittleEndian, &displayWindow)
		case "lineOrder":
			err = binary.Read(r, binary.LittleEndian, &lineOrder)
		case "pixelAspectRatio":
			err = binary.Read(r, binary.LittleEndian, &pixelAspectRatio)
		case "screenWindowCenter":
			err = binary.Read(r, binary.LittleEndian, &screenWindowCenter)
		case "screenWindowWidth":
			err = binary.Read(r, binary.LittleEndian, &screenWindowWidth)
		default:
			var data []byte = make([]byte, size)
			err = binary.Read(r, binary.LittleEndian, data)
		}

		if err != nil {
			return nil, err
		}

		index := slices.Index(requiredFields, name[:len(name)-1])

		if index != -1 {
			requiredFields = append(requiredFields[:index], requiredFields[index+1:]...)
		}

		name, err = r.ReadString(0)
	}

	if err != nil {
		return nil, err
	}

	if len(requiredFields) > 0 {
		return nil, fmt.Errorf("exr missing required fields %v", requiredFields)
	}

	lineCount := (dataWindow.YMax - dataWindow.YMin + 1)
	scanlineCount := lineCount / uint32(compression.LineCount())
	if lineCount%uint32(compression.LineCount()) != 0 {
		scanlineCount += 1
	}

	var offsetTable []uint64 = make([]uint64, scanlineCount)
	err = binary.Read(r, binary.LittleEndian, offsetTable)
	if err != nil {
		return nil, err
	}
	return &OpenEXRHeader{
		Magic:              magic,
		Version:            version_flags[0],
		Flags:              [3]uint8(version_flags[1:]),
		Channels:           channels,
		Compression:        compression,
		DataWindow:         dataWindow,
		DisplayWindow:      displayWindow,
		LineOrder:          lineOrder,
		PixelAspectRatio:   pixelAspectRatio,
		ScreenWindowCenter: screenWindowCenter,
		ScreenWindowWidth:  screenWindowWidth,
		OffsetTable:        offsetTable,
	}, nil
}

func LoadOpenEXR(r bufio.Reader) (*OpenEXR, error) {
	header, err := loadEXRHeader(&r)
	if err != nil {
		return nil, err
	}
	height := (header.DataWindow.YMax - header.DataWindow.YMin + 1)
	width := (header.DataWindow.YMax - header.DataWindow.YMin + 1)

	pixelSize := 0
	for _, channel := range header.Channels {
		pixelSize += channel.PixelFmt.Size()
	}

	scanlineCount := len(header.OffsetTable)
	scanlines := make([]ScanLine, 0, scanlineCount)
	for i := 0; i < int(scanlineCount); i++ {
		var scanline ScanLine
		err = binary.Read(&r, binary.LittleEndian, &scanline.YCoord)
		if err != nil {
			return nil, err
		}

		err = binary.Read(&r, binary.LittleEndian, &scanline.Size)
		if err != nil {
			return nil, err
		}

		scanline.Data = make([]uint8, scanline.Size)
		err = binary.Read(&r, binary.LittleEndian, &scanline.Data)
		if err != nil {
			return nil, err
		}

		lineCount := min(height, uint32(header.Compression.LineCount()))
		height -= uint32(header.Compression.LineCount())

		scanline.Compressed = lineCount*width*uint32(pixelSize) > scanline.Size
		scanline.LineCount = lineCount

		scanlines = append(scanlines, scanline)
	}

	return &OpenEXR{
		OpenEXRHeader: *header,
		ScanLines:     scanlines,
	}, nil
}

func dumpAttribute(w io.Writer, name string, typ string, data []byte) error {
	str := fmt.Sprintf("%s\x00%s\x00", name, typ)
	err := binary.Write(w, binary.LittleEndian, []byte(str))
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, uint32(len(data)))
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, data)
	if err != nil {
		return err
	}
	return nil
}

func dumpAttributes(w io.Writer, exr *OpenEXR) error {
	attrBuf := &bytes.Buffer{}
	for _, channel := range exr.Channels {
		err := binary.Write(attrBuf, binary.LittleEndian, []byte(fmt.Sprintf("%s\x00", channel.Name)))
		if err != nil {
			return err
		}

		err = binary.Write(attrBuf, binary.LittleEndian, channel.PixelFmt)
		if err != nil {
			return err
		}

		err = binary.Write(attrBuf, binary.LittleEndian, channel.Linear)
		if err != nil {
			return err
		}

		err = binary.Write(attrBuf, binary.LittleEndian, channel.XSampling)
		if err != nil {
			return err
		}

		err = binary.Write(attrBuf, binary.LittleEndian, channel.YSampling)
		if err != nil {
			return err
		}
	}
	err := binary.Write(attrBuf, binary.LittleEndian, byte(0))
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "channels", "chlist", attrBuf.Bytes())
	if err != nil {
		return err
	}

	attrBuf.Reset()
	err = binary.Write(attrBuf, binary.LittleEndian, exr.Compression)
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "compression", "compression", attrBuf.Bytes())
	if err != nil {
		return err
	}

	attrBuf.Reset()
	err = binary.Write(attrBuf, binary.LittleEndian, exr.DataWindow)
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "dataWindow", "box2i", attrBuf.Bytes())
	if err != nil {
		return err
	}

	attrBuf.Reset()
	err = binary.Write(attrBuf, binary.LittleEndian, exr.DisplayWindow)
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "displayWindow", "box2i", attrBuf.Bytes())
	if err != nil {
		return err
	}

	attrBuf.Reset()
	err = binary.Write(attrBuf, binary.LittleEndian, exr.LineOrder)
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "lineOrder", "lineOrder", attrBuf.Bytes())
	if err != nil {
		return err
	}

	attrBuf.Reset()
	err = binary.Write(attrBuf, binary.LittleEndian, exr.PixelAspectRatio)
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "pixelAspectRatio", "float", attrBuf.Bytes())
	if err != nil {
		return err
	}

	attrBuf.Reset()
	err = binary.Write(attrBuf, binary.LittleEndian, exr.ScreenWindowCenter)
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "screenWindowCenter", "v2f", attrBuf.Bytes())
	if err != nil {
		return err
	}

	attrBuf.Reset()
	err = binary.Write(attrBuf, binary.LittleEndian, exr.ScreenWindowWidth)
	if err != nil {
		return err
	}

	err = dumpAttribute(w, "screenWindowWidth", "float", attrBuf.Bytes())
	if err != nil {
		return err
	}

	err = binary.Write(w, binary.LittleEndian, byte(0))
	if err != nil {
		return err
	}

	return nil
}

func (exr *OpenEXR) dump(w io.WriteSeeker) error {
	err := binary.Write(w, binary.LittleEndian, exr.Magic)
	if err != nil {
		return err
	}

	err = binary.Write(w, binary.LittleEndian, [4]byte{exr.Version, 0, 0, 0})
	if err != nil {
		return err
	}

	err = dumpAttributes(w, exr)
	if err != nil {
		return err
	}

	offset, err := w.Seek(0, io.SeekCurrent)
	if err != nil {
		return err
	}

	offset += int64(8 * len(exr.ScanLines))
	for i := range exr.ScanLines {
		err = binary.Write(w, binary.LittleEndian, uint64(offset))
		if err != nil {
			return err
		}

		if !exr.ScanLines[i].Compressed && exr.Compression != CompressionNone {
			err = exr.ScanLines[i].Compress(exr.Compression)
			if err != nil {
				return err
			}
		}
		offset += int64(8 + exr.ScanLines[i].Size)
	}

	for i, scanline := range exr.ScanLines {
		err = binary.Write(w, binary.LittleEndian, scanline.YCoord)
		if err != nil {
			return err
		}

		err = binary.Write(w, binary.LittleEndian, uint32(len(exr.ScanLines[i].Data)))
		if err != nil {
			return err
		}

		err = binary.Write(w, binary.LittleEndian, scanline.Data)
		if err != nil {
			return err
		}
	}

	return nil
}

func openEXRFromHDRImage(img image.Image) (*OpenEXR, error) {
	var (
		channels           []Channel   = make([]Channel, 4)
		compression        Compression = CompressionZIP
		dataWindow         Box2i
		displayWindow      Box2i
		lineOrder          LineOrder  = OrderIncreasingY
		pixelAspectRatio   float32    = 1.0
		screenWindowCenter [2]float32 = [2]float32{0, 0}
		screenWindowWidth  float32    = 1.0
	)

	dataWindow = Box2i{
		XMin: uint32(img.Bounds().Min.X),
		XMax: uint32(img.Bounds().Max.X - 1),
		YMin: uint32(img.Bounds().Min.Y),
		YMax: uint32(img.Bounds().Max.Y - 1),
	}

	displayWindow = dataWindow

	var pixelFmt PixelType
	switch img.ColorModel() {
	case hdrColors.NRGBA128UModel:
		pixelFmt = TypeUInt
	case hdrColors.NRGBA64FModel:
		pixelFmt = TypeHalf
	case hdrColors.NRGBA128FModel:
		pixelFmt = TypeFloat
	default:
		pixelFmt = TypeFloat
	}

	for i := range channels {
		channels[i].Linear = 0
		channels[i].XSampling = 1
		channels[i].YSampling = 1
		channels[i].Name = []string{"A", "B", "G", "R"}[i]
		channels[i].PixelFmt = pixelFmt
	}

	var pixels []byte
	var offsetFunc func(x, y int) int
	switch img.ColorModel() {
	case hdrColors.NRGBA128UModel:
		imgView, ok := img.(*hdrColors.NRGBA128UImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				return nil, fmt.Errorf("could not convert image to PixelType uint")
			}
			imgView, ok = ddsImg.Image.(*hdrColors.NRGBA128UImage)
			if !ok {
				return nil, fmt.Errorf("could not convert image to PixelType uint")
			}
		}
		pixels = imgView.Pix
		offsetFunc = func(x, y int) int {
			return imgView.PixOffset(x, y)
		}
	case hdrColors.NRGBA64FModel:
		imgView, ok := img.(*hdrColors.NRGBA64FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				return nil, fmt.Errorf("could not convert image to PixelType half")
			}
			imgView, ok = ddsImg.Image.(*hdrColors.NRGBA64FImage)
			if !ok {
				return nil, fmt.Errorf("could not convert image to PixelType half")
			}
		}
		pixels = imgView.Pix
		offsetFunc = func(x, y int) int {
			return imgView.PixOffset(x, y)
		}
	case hdrColors.NRGBA128FModel:
		imgView, ok := img.(*hdrColors.NRGBA128FImage)
		if !ok {
			ddsImg, ok := img.(*dds.DDS)
			if !ok {
				return nil, fmt.Errorf("could not convert image to PixelType float")
			}
			imgView, ok = ddsImg.Image.(*hdrColors.NRGBA128FImage)
			if !ok {
				return nil, fmt.Errorf("could not convert image to PixelType float")
			}
		}
		pixels = imgView.Pix
		offsetFunc = func(x, y int) int {
			return imgView.PixOffset(x, y)
		}
	default:
		return nil, fmt.Errorf("not currently implemented")
	}

	var scanlines []ScanLine = make([]ScanLine, 0, 1)
	uncompressedSize := uint32(compression.LineCount()) * dataWindow.Width() * uint32(pixelFmt.Size()) * uint32(len(channels))
	scanline := ScanLine{
		YCoord:     0,
		Size:       uncompressedSize,
		LineCount:  uint32(compression.LineCount()),
		Compressed: false,
		Data:       make([]uint8, uncompressedSize),
	}
	for row := 0; row < int(dataWindow.Height()); row++ {
		for channel := 0; channel < len(channels); channel++ {
			for column := 0; column < int(dataWindow.Width()); column++ {
				scanOffset := scanline.offset(column, row, channel, len(channels), pixelFmt, dataWindow)
				scanEnd := scanOffset + int64(pixelFmt.Size())

				pixOffset := offsetFunc(column, row) + (3-channel)*pixelFmt.Size()
				pixEnd := pixOffset + pixelFmt.Size()
				copy(scanline.Data[scanOffset:scanEnd], pixels[pixOffset:pixEnd])
			}
		}

		if uint32(row) == scanline.YCoord+scanline.LineCount-1 && row < int(dataWindow.Height())-1 {
			scanlines = append(scanlines, scanline)
			scanline = ScanLine{
				YCoord:     uint32(row + 1),
				Size:       uncompressedSize,
				LineCount:  uint32(compression.LineCount()),
				Compressed: false,
				Data:       make([]uint8, uncompressedSize),
			}
		}
	}
	scanlines = append(scanlines, scanline)

	return &OpenEXR{
		OpenEXRHeader: OpenEXRHeader{
			Magic:              EXR_MAGIC,
			Version:            2,
			Flags:              [3]uint8{0, 0, 0},
			Channels:           channels,
			Compression:        compression,
			DataWindow:         dataWindow,
			DisplayWindow:      displayWindow,
			LineOrder:          lineOrder,
			PixelAspectRatio:   pixelAspectRatio,
			ScreenWindowCenter: screenWindowCenter,
			ScreenWindowWidth:  screenWindowWidth,
			OffsetTable:        make([]uint64, len(scanlines)),
		},
		ScanLines: scanlines,
	}, nil
}

func WriteHDR(w io.WriteSeeker, img image.Image) error {
	exr, err := openEXRFromHDRImage(img)
	if err != nil {
		return err
	}
	err = exr.dump(w)
	return err
}

func reconstruct(data []byte) []byte {
	output := make([]byte, len(data))
	output[0] = data[0]
	for i := 1; i < len(data); i++ {
		output[i] = byte(int(output[i-1]) + int(data[i]) - 128)
	}
	return output
}

func interleave(data []byte) []byte {
	output := make([]byte, len(data))
	for i := 0; i < len(data)/2; i++ {
		output[i*2] = data[i]
		output[i*2+1] = data[i+(len(data)+1)/2]
	}
	return output
}

func deconstruct(data []byte) []byte {
	output := make([]byte, len(data))
	output[0] = data[0]
	p := int(data[0])
	for i := 1; i < len(data); i++ {
		output[i] = byte(int(data[i]) - p + 0x180)
		p = int(data[i])
	}
	return output
}

func reorder(data []byte) []byte {
	output := make([]byte, len(data))
	for i := 0; i < len(data)/2; i++ {
		output[i] = data[i*2]
		output[i+(len(data)+1)/2] = data[i*2+1]
	}
	return output
}

func decompressZip(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	decompressed, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	reconstructed := reconstruct(decompressed)
	return interleave(reconstructed), nil
}

func decompressNone(data []byte) ([]byte, error) {
	return data, nil
}

func decompressNotImplemented(data []byte) ([]byte, error) {
	return nil, fmt.Errorf("unimplemented compression scheme")
}

func (scanline *ScanLine) Decompress(compression Compression) error {
	if !scanline.Compressed {
		return nil
	}

	var decompressFn func([]byte) ([]byte, error)
	switch compression {
	case CompressionNone:
		decompressFn = decompressNone
	case CompressionZIPS:
		fallthrough
	case CompressionZIP:
		decompressFn = decompressZip
	default:
		decompressFn = decompressNotImplemented
	}

	data, err := decompressFn(scanline.Data)
	if err != nil {
		return err
	}
	scanline.Data = data
	scanline.Compressed = false

	return nil
}

func compressZip(data []byte) ([]byte, error) {
	reordered := reorder(data)
	deconstructed := deconstruct(reordered)
	compressed := bytes.Buffer{}
	w := zlib.NewWriter(&compressed)
	n, err := w.Write(deconstructed)
	if err != nil {
		return nil, err
	}
	err = w.Close()
	if err != nil {
		return nil, err
	}
	return compressed.Next(n), nil
}

func compressNone(data []byte) ([]byte, error) {
	return data, nil
}

func compressNotImplemented(_ []byte) ([]byte, error) {
	return nil, fmt.Errorf("unimplemented compression scheme")
}

func (scanline *ScanLine) Compress(compression Compression) error {
	if scanline.Compressed {
		return nil
	}

	var compressFn func([]byte) ([]byte, error)
	switch compression {
	case CompressionNone:
		compressFn = compressNone
	case CompressionZIPS:
		fallthrough
	case CompressionZIP:
		compressFn = compressZip
	default:
		compressFn = compressNotImplemented
	}

	data, err := compressFn(scanline.Data)
	if err != nil {
		return err
	}
	if len(data) < len(scanline.Data) {
		scanline.Data = data
	}
	if len(scanline.Data) != int(scanline.Size) {
		scanline.Size = uint32(len(scanline.Data))
	}
	scanline.Compressed = true

	return nil
}

func (exr *OpenEXR) Pixels() ([][][4]float32, error) {
	height := exr.DataWindow.YMax - exr.DataWindow.YMin + 1
	width := exr.DataWindow.XMax - exr.DataWindow.XMin + 1
	depth := uint32(len(exr.Channels))

	output := make([][][4]float32, height)

	for _, scanline := range exr.ScanLines {
		if err := scanline.Decompress(exr.Compression); err != nil {
			return nil, err
		}

		r := bytes.NewReader(scanline.Data)

		for i := uint32(0); i < scanline.LineCount; i++ {
			output[scanline.YCoord+i] = make([][4]float32, width)
			for j := uint32(0); j < depth; j++ {
				values := make([]float32, width)
				binary.Read(r, binary.LittleEndian, values)
				for k := uint32(0); k < width; k++ {
					output[scanline.YCoord+i][k][depth-j-1] = values[k]
				}
			}
		}
	}

	if depth == 3 {
		for i := 0; i < len(output); i++ {
			for j := 0; j < len(output[i]); j++ {
				output[i][j][3] = 1.0
			}
		}
	}

	return output, nil
}

func (exr *OpenEXR) HdrImage() (image.Image, error) {
	width := exr.DataWindow.XMax - exr.DataWindow.XMin + 1
	depth := uint32(len(exr.Channels))

	var img image.Image
	var buf []uint8
	var one []uint8
	var translatePixel func(r *bytes.Reader, x, y, channel int)
	switch exr.ColorModel() {
	case hdrColors.NRGBA128UModel:
		newImg := hdrColors.NewNRGBA128UImage(exr.Bounds())
		buf = newImg.Pix
		img = newImg
		translatePixel = func(r *bytes.Reader, x, y, channel int) {
			var val uint32
			if err := binary.Read(r, binary.LittleEndian, &val); err != nil {
				panic(err)
			}
			offset := newImg.PixOffset(x, y)
			binary.Encode(buf[offset+channel*4:], binary.LittleEndian, val)
		}
		one = make([]uint8, 4)
		binary.Encode(one, binary.LittleEndian, uint32(0xFFFFFFFF))
	case hdrColors.NRGBA64FModel:
		newImg := hdrColors.NewNRGBA64FImage(exr.Bounds())
		buf = newImg.Pix
		img = newImg
		translatePixel = func(r *bytes.Reader, x, y, channel int) {
			var val float16.Float16
			if err := binary.Read(r, binary.LittleEndian, &val); err != nil {
				panic(err)
			}
			offset := newImg.PixOffset(x, y)
			binary.Encode(buf[offset+channel*2:], binary.LittleEndian, val)
		}
		one = make([]uint8, 2)
		binary.Encode(one, binary.LittleEndian, float16.Fromfloat32(1.0))
	case hdrColors.NRGBA128FModel:
		newImg := hdrColors.NewNRGBA128FImage(exr.Bounds())
		buf = newImg.Pix
		img = newImg
		translatePixel = func(r *bytes.Reader, x, y, channel int) {
			var val float32
			if err := binary.Read(r, binary.LittleEndian, &val); err != nil {
				panic(err)
			}
			offset := newImg.PixOffset(x, y)
			binary.Encode(buf[offset+channel*4:], binary.LittleEndian, val)
		}
		one = make([]uint8, 4)
		binary.Encode(one, binary.LittleEndian, 1.0)
	}

	for _, scanline := range exr.ScanLines {
		if err := scanline.Decompress(exr.Compression); err != nil {
			return nil, err
		}

		r := bytes.NewReader(scanline.Data)

		for i := 0; i < int(scanline.LineCount); i++ {
			for j := 0; j < int(depth); j++ {
				for k := 0; k < int(width); k++ {
					x, y := k, int(scanline.YCoord)+i
					r.Seek(scanline.offset(x, y, j, int(depth), exr.Channels[j].PixelFmt, exr.DataWindow), io.SeekStart)
					translatePixel(r, x, y, int(depth)-j-1)
					if j == 2 && depth == 3 {
						rone := bytes.NewReader(one)
						translatePixel(rone, x, y, 3)
					}
				}
			}
		}
	}

	return img, nil
}

func (exr *OpenEXR) At(x, y int) color.Color {
	var index int
	for i, scanline := range exr.ScanLines {
		if scanline.YCoord <= uint32(y) && (scanline.YCoord+scanline.LineCount) > uint32(y) {
			index = i
			break
		}
	}

	err := exr.ScanLines[index].Decompress(exr.Compression)
	if err != nil {
		panic(err)
	}

	pixelFmt := exr.Channels[0].PixelFmt

	var pixel color.Color
	switch pixelFmt {
	case TypeUInt:
		pixel = &hdrColors.NRGBA128U{R: 0, G: 0, B: 0, A: 0xFFFFFFFF}
	case TypeHalf:
		pixel = &hdrColors.NRGBA64F{R: 0, G: 0, B: 0, A: float16.Fromfloat32(1.0)}
	case TypeFloat:
		pixel = &hdrColors.NRGBA128F{R: 0, G: 0, B: 0, A: 1.0}
	default:
		panic(fmt.Errorf("unknown pixel format"))
	}
	depth := len(exr.Channels)
	r := bytes.NewReader(exr.ScanLines[index].Data)
	for i, channel := range exr.Channels {
		if channel.PixelFmt != pixelFmt {
			panic(fmt.Errorf("channel %s had pixel type %s, expected %s", channel.Name, channel.PixelFmt.String(), pixelFmt.String()))
		}

		offset := exr.ScanLines[index].offset(x, y, i, depth, channel.PixelFmt, exr.DataWindow)
		r.Seek(offset, io.SeekStart)
		switch channel.PixelFmt {
		case TypeUInt:
			px, ok := pixel.(*hdrColors.NRGBA128U)
			if !ok {
				panic(fmt.Errorf("failed to convert pixel"))
			}
			binary.Read(r, binary.LittleEndian, px.Channel(channel.Name))
		case TypeHalf:
			px, ok := pixel.(*hdrColors.NRGBA64F)
			if !ok {
				panic(fmt.Errorf("failed to convert pixel"))
			}
			binary.Read(r, binary.LittleEndian, px.Channel(channel.Name))
		case TypeFloat:
			px, ok := pixel.(*hdrColors.NRGBA128F)
			if !ok {
				panic(fmt.Errorf("failed to convert pixel"))
			}
			binary.Read(r, binary.LittleEndian, px.Channel(channel.Name))
		}
	}
	return pixel
}

func (exr *OpenEXR) ColorModel() color.Model {
	return exr.Channels[0].PixelFmt.Model()
}

func (exr *OpenEXR) Bounds() image.Rectangle {
	return image.Rect(int(exr.DataWindow.XMin), int(exr.DataWindow.YMin), int(exr.DataWindow.XMax+1), int(exr.DataWindow.YMax+1))
}

func Decode(r io.Reader) (image.Image, error) {
	bufR := bufio.NewReader(r)
	exr, err := LoadOpenEXR(*bufR)
	if err != nil {
		return nil, err
	}
	return exr, nil
}

func DecodeConfig(r io.Reader) (image.Config, error) {
	bufR := bufio.NewReader(r)
	header, err := loadEXRHeader(bufR)
	if err != nil {
		return image.Config{}, err
	}

	return image.Config{
		ColorModel: header.Channels[0].PixelFmt.Model(),
		Width:      int(header.DataWindow.XMax - header.DataWindow.XMin + 1),
		Height:     int(header.DataWindow.YMax - header.DataWindow.YMin + 1),
	}, nil
}

func init() {
	image.RegisterFormat("exr", "v/1\x01", Decode, DecodeConfig)
}
