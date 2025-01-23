package openexr

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"slices"
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

type Box2f struct {
	XMin float32
	YMin float32
	XMax float32
	YMax float32
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
	YCoord uint32
	Size   uint32
	Data   []uint8
}

type OpenEXR struct {
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
	ScanLines          []ScanLine
}

func loadChannels(r bufio.Reader) ([]Channel, error) {
	channels := make([]Channel, 0, 4)

	name, err := r.ReadString(0)
	for len(name) > 0 {
		if err != nil {
			return nil, err
		}

		var pixelFmt PixelType
		err = binary.Read(&r, binary.LittleEndian, &pixelFmt)
		if err != nil {
			return nil, err
		}

		var pLinear uint32
		err = binary.Read(&r, binary.LittleEndian, &pLinear)
		if err != nil {
			return nil, err
		}

		var xSampling uint32
		err = binary.Read(&r, binary.LittleEndian, &xSampling)
		if err != nil {
			return nil, err
		}

		var ySampling uint32
		err = binary.Read(&r, binary.LittleEndian, &ySampling)
		if err != nil {
			return nil, err
		}

		channels = append(channels, Channel{
			Name:      name,
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

func LoadOpenEXR(r bufio.Reader) (*OpenEXR, error) {
	var magic uint32
	if err := binary.Read(&r, binary.LittleEndian, &magic); err != nil {
		return nil, err
	}

	if magic != EXR_MAGIC {
		return nil, fmt.Errorf("invalid file magic %v", magic)
	}

	var version_flags [4]uint8
	if err := binary.Read(&r, binary.LittleEndian, &version_flags); err != nil {
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
	for len(name) > 0 {
		if err != nil {
			return nil, err
		}

		_, err = r.ReadString(0)
		if err != nil {
			return nil, err
		}

		var size uint32
		err = binary.Read(&r, binary.LittleEndian, &size)
		if err != nil {
			return nil, err
		}

		switch name {
		case "channels":
			channels, err = loadChannels(r)
		case "compression":
			err = binary.Read(&r, binary.LittleEndian, &compression)
		case "dataWindow":
			err = binary.Read(&r, binary.LittleEndian, &dataWindow)
		case "displayWindow":
			err = binary.Read(&r, binary.LittleEndian, &displayWindow)
		case "lineOrder":
			err = binary.Read(&r, binary.LittleEndian, &lineOrder)
		case "pixelAspectRatio":
			err = binary.Read(&r, binary.LittleEndian, &pixelAspectRatio)
		case "screenWindowCenter":
			err = binary.Read(&r, binary.LittleEndian, &screenWindowCenter)
		case "screenWindowWidth":
			err = binary.Read(&r, binary.LittleEndian, &screenWindowWidth)
		default:
			var data []byte = make([]byte, size)
			err = binary.Read(&r, binary.LittleEndian, data)
		}

		if err != nil {
			return nil, err
		}

		index := slices.Index(requiredFields, name)

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
	err = binary.Read(&r, binary.LittleEndian, offsetTable)
	if err != nil {
		return nil, err
	}

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

		scanlines = append(scanlines, scanline)
	}

	return &OpenEXR{
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
		ScanLines:          scanlines,
	}, nil
}

func (exr *OpenEXR) Dump(w io.Writer) error {
	if err := binary.Write(w, binary.LittleEndian, EXR_MAGIC); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(2)); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("channels\x00chlist\x00")); err != nil {
		return err
	}

	// Total size is null terminating byte plus length of names and their null bytes plus 16 times number of channels
	channelListSize := uint32(1 + 17*len(exr.Channels))
	for _, channel := range exr.Channels {
		channelListSize += uint32(len(channel.Name))
	}

	if err := binary.Write(w, binary.LittleEndian, channelListSize); err != nil {
		return err
	}

	for _, channel := range exr.Channels {
		if err := binary.Write(w, binary.LittleEndian, []byte(channel.Name+"\x00")); err != nil {
			return err
		}

		if err := binary.Write(w, binary.LittleEndian, channel.PixelFmt); err != nil {
			return err
		}

		if err := binary.Write(w, binary.LittleEndian, channel.Linear); err != nil {
			return err
		}

		if err := binary.Write(w, binary.LittleEndian, channel.XSampling); err != nil {
			return err
		}

		if err := binary.Write(w, binary.LittleEndian, channel.YSampling); err != nil {
			return err
		}
	}
	if err := binary.Write(w, binary.LittleEndian, byte(0)); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("compression\x00compression\x00")); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(binary.Size(exr.Compression))); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, exr.Compression); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("dataWindow\x00box2i\x00")); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(binary.Size(exr.DataWindow))); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, exr.DataWindow); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("displayWindow\x00box2i\x00")); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(binary.Size(exr.DisplayWindow))); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, exr.DisplayWindow); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("lineOrder\x00lineOrder\x00")); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(binary.Size(exr.LineOrder))); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, exr.LineOrder); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("pixelAspectRatio\x00float\x00")); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(binary.Size(exr.PixelAspectRatio))); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, exr.PixelAspectRatio); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("screenWindowCenter\x00v2f\x00")); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(binary.Size(exr.ScreenWindowCenter))); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, exr.ScreenWindowCenter); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, []byte("screenWindowWidth\x00float\x00")); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, uint32(binary.Size(exr.ScreenWindowWidth))); err != nil {
		return err
	}

	if err := binary.Write(w, binary.LittleEndian, exr.ScreenWindowWidth); err != nil {
		return err
	}

	return nil
}
