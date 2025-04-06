package dds

import (
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"strconv"

	"github.com/ryanjsims/hd2-lut-editor/hdrColors"
)

type Info struct {
	Header      Header
	DXT10Header *DXT10Header
	Decompress  DecompressFunc
	ColorModel  color.Model
	NumMipMaps  int
	NumImages   int
}

func StackLayers(origTex *DDS) *DDS {
	var img image.Image
	var pxBuf []uint8
	width, height := origTex.Bounds().Dx(), len(origTex.Images)*origTex.Bounds().Dy()
	switch origTex.Info.ColorModel {
	case color.GrayModel:
		newImg := image.NewGray(image.Rect(0, 0, width, height))
		pxBuf = newImg.Pix
		img = newImg
	case color.Gray16Model:
		newImg := image.NewGray16(image.Rect(0, 0, width, height))
		pxBuf = newImg.Pix
		img = newImg
	case color.NRGBAModel:
		newImg := image.NewNRGBA(image.Rect(0, 0, width, height))
		pxBuf = newImg.Pix
		img = newImg
	case color.NRGBA64Model:
		newImg := image.NewNRGBA64(image.Rect(0, 0, width, height))
		pxBuf = newImg.Pix
		img = newImg
	}
	offset := 0
	for _, layer := range origTex.Images {
		if layer.Bounds().Dx() != img.Bounds().Dx() {
			continue
		}
		var layerPxBuf []uint8
		switch layer.ColorModel() {
		case color.GrayModel:
			m, _ := layer.Image.(*image.Gray)
			layerPxBuf = m.Pix
		case color.Gray16Model:
			m, _ := layer.Image.(*image.Gray16)
			layerPxBuf = m.Pix
		case color.NRGBAModel:
			m, _ := layer.Image.(*image.NRGBA)
			layerPxBuf = m.Pix
		case color.NRGBA64Model:
			m, _ := layer.Image.(*image.NRGBA64)
			layerPxBuf = m.Pix
		}
		offset += copy(pxBuf[offset:], layerPxBuf[:])
	}

	mipMaps := make([]*DDSMipMap, 1)
	mipMaps[0] = &DDSMipMap{
		Image:  img,
		Width:  width,
		Height: height,
	}
	images := make([]*DDSImage, 1)
	images[0] = &DDSImage{
		Image:   img,
		MipMaps: mipMaps,
	}

	return &DDS{
		Image:  img,
		Images: images,
	}
}

func DecodeInfo(r io.Reader) (Info, error) {
	hdr, err := DecodeHeader(r)
	if err != nil {
		return Info{}, err
	}

	info := Info{
		Header:     hdr,
		NumMipMaps: 1,
	}

	cubemap := hdr.Caps2&Caps2Cubemap != 0
	volume := hdr.Caps2&Caps2Volume != 0 && hdr.Depth > 0

	if hdr.PixelFormat.Flags&PixelFormatFlagRGB != 0 {
		info.ColorModel = color.NRGBAModel
		info.Decompress = DecompressUncompressed
	} else if hdr.PixelFormat.Flags&PixelFormatFlagYUV != 0 {
		if hdr.PixelFormat.Flags&PixelFormatFlagAlphaPixels == 0 {
			info.ColorModel = color.YCbCrModel
		} else {
			info.ColorModel = color.NYCbCrAModel
		}
		info.Decompress = DecompressUncompressed
	} else if hdr.PixelFormat.Flags&PixelFormatFlagLuminance != 0 {
		if hdr.PixelFormat.Flags&PixelFormatFlagAlphaPixels == 0 {
			if hdr.PixelFormat.GBitMask == 0 && hdr.PixelFormat.BBitMask == 0 {
				if hdr.PixelFormat.RGBBitCount > 8 {
					info.ColorModel = color.Gray16Model
				} else {
					info.ColorModel = color.GrayModel
				}
			} else {
				info.ColorModel = color.NRGBAModel
			}
		} else {
			info.ColorModel = color.NRGBAModel
		}
		info.Decompress = DecompressUncompressed
	} else if hdr.PixelFormat.Flags&PixelFormatFlagFourCC != 0 {
		switch hdr.PixelFormat.FourCC {
		case [4]byte{'A', 'T', 'I', '1'}:
			info.ColorModel = color.GrayModel
			info.Decompress = Decompress3DcPlus
		case [4]byte{'A', 'T', 'I', '2'}:
			info.ColorModel = color.NRGBAModel
			info.Decompress = Decompress3Dc
		case [4]byte{'D', 'X', 'T', '1'}:
			info.ColorModel = color.NRGBAModel
			info.Decompress = DecompressDXT1
		case [4]byte{'D', 'X', 'T', '3'}:
			return Info{}, errors.New("DXT3 compression unsupported")
		case [4]byte{'D', 'X', 'T', '5'}:
			info.ColorModel = color.NRGBAModel
			info.Decompress = DecompressDXT5
		case [4]byte{'D', 'X', '1', '0'}:
			dx10, err := DecodeDXT10Header(r)
			if err != nil {
				return Info{}, err
			}
			info.DXT10Header = &dx10

			if dx10.ResourceDimension != D3D10ResourceDimensionTexture2D {
				return Info{}, errors.New("unsupported DXT10 resource dimension")
			}

			switch dx10.DXGIFormat {
			case DXGIFormatR32G32B32A32Float,
				DXGIFormatR32G32B32Float:
				info.ColorModel = hdrColors.NRGBA128FModel
				info.Decompress = DecompressUncompressedDXT10
			case DXGIFormatR16G16B16A16Float,
				DXGIFormatR16G16B16A16UNorm:
				info.ColorModel = hdrColors.NRGBA64FModel
				info.Decompress = DecompressUncompressedDXT10
			case DXGIFormatR32G32Float:
				info.ColorModel = color.NRGBA64Model
				info.Decompress = DecompressUncompressedDXT10
			case DXGIFormatR32Float, DXGIFormatR16UNorm:
				info.ColorModel = color.Gray16Model
				info.Decompress = DecompressUncompressedDXT10
			case DXGIFormatR8UNorm:
				info.ColorModel = color.GrayModel
				info.Decompress = DecompressUncompressedDXT10
			case DXGIFormatR8G8B8A8UNorm:
				info.ColorModel = color.NRGBAModel
				info.Decompress = DecompressUncompressedDXT10
			case DXGIFormatBC1UNorm:
				info.ColorModel = color.NRGBAModel
				info.Decompress = DecompressDXT1
			case DXGIFormatBC2UNorm:
				return Info{}, errors.New("DXT3 compression unsupported")
			case DXGIFormatBC3UNorm:
				info.ColorModel = color.NRGBAModel
				info.Decompress = DecompressDXT5
			case DXGIFormatBC4UNorm:
				info.ColorModel = color.GrayModel
				info.Decompress = Decompress3DcPlus
			case DXGIFormatBC5UNorm:
				info.ColorModel = color.NRGBAModel
				info.Decompress = Decompress3Dc
			case DXGIFormatBC7UNorm:
				info.ColorModel = color.NRGBAModel
				info.Decompress = DecompressBC7
			case DXGIFormatBC7UNormSRGB:
				return Info{}, errors.New("BC7 SRGB compression unsupported")
			default:
				return Info{}, fmt.Errorf("unsupported DXGI format: %v", dx10.DXGIFormat)
			}

			if dx10.MiscFlag&D3D10ResourceMiscFlagTextureCube != 0 {
				cubemap = true
			}
		default:
			return Info{}, fmt.Errorf("unsupported cmpression format: unknown fourCC: %v", strconv.Quote(string(hdr.PixelFormat.FourCC[:])))
		}
	}

	info.NumImages = 1

	if info.DXT10Header != nil {
		info.NumImages = int(info.DXT10Header.ArraySize)
	}

	if cubemap {
		info.NumImages = 0
		if hdr.Caps2&Caps2CubemapPlusX != 0 {
			info.NumImages++
		}
		if hdr.Caps2&Caps2CubemapMinusX != 0 {
			info.NumImages++
		}
		if hdr.Caps2&Caps2CubemapPlusY != 0 {
			info.NumImages++
		}
		if hdr.Caps2&Caps2CubemapMinusY != 0 {
			info.NumImages++
		}
		if hdr.Caps2&Caps2CubemapPlusZ != 0 {
			info.NumImages++
		}
		if hdr.Caps2&Caps2CubemapMinusZ != 0 {
			info.NumImages++
		}
	}

	if volume {
		info.NumImages = int(hdr.Depth)
	}

	if info.NumImages == 0 {
		return Info{}, errors.New("invalid image header: no images")
	}

	if hdr.Caps&CapsMipMap != 0 &&
		(hdr.Caps&CapsTexture != 0 || hdr.Caps2&Caps2Cubemap != 0) {
		info.NumMipMaps = int(hdr.MipMapCount)
	}

	if info.NumMipMaps == 0 {
		return Info{}, errors.New("invalid image header: base image mipmap (mip 0) missing")
	}

	return info, nil
}

func DecodeConfig(r io.Reader) (image.Config, error) {
	info, err := DecodeInfo(r)
	if err != nil {
		return image.Config{}, err
	}
	return image.Config{
		ColorModel: info.ColorModel,
		Width:      int(info.Header.Width),
		Height:     int(info.Header.Height),
	}, nil
}

type DDSMipMap struct {
	image.Image
	Width, Height int
}

type DDSImage struct {
	image.Image
	// Length is guaranteed to be >= 1.
	MipMaps []*DDSMipMap
}

type DDS struct {
	image.Image
	Info Info
	// Length is guaranteed to be >= 1.
	Images []*DDSImage
}

func WriteHDR(w io.Writer, hdrImg image.Image) error {
	ddsImg, ok := hdrImg.(*DDS)
	if ok {
		return ddsImg.dump(w)
	}

	var dxgiFmt DXGIFormat
	var pix []byte
	switch hdrImg.ColorModel() {
	case hdrColors.NRGBA128FModel:
		dxgiFmt = DXGIFormatR32G32B32A32Float
		img, ok := hdrImg.(*hdrColors.NRGBA128FImage)
		if !ok {
			return fmt.Errorf("failed to convert dds to NRGBA128F")
		}
		pix = img.Pix
	case hdrColors.NRGBA128UModel:
		dxgiFmt = DXGIFormatR32G32B32A32UInt
		img, ok := hdrImg.(*hdrColors.NRGBA128UImage)
		if !ok {
			return fmt.Errorf("failed to convert dds to NRGBA128U")
		}
		pix = img.Pix
	case hdrColors.NRGBA64FModel:
		dxgiFmt = DXGIFormatR16G16B16A16Float
		img, ok := hdrImg.(*hdrColors.NRGBA64FImage)
		if !ok {
			return fmt.Errorf("failed to convert dds to NRGBA64F")
		}
		pix = img.Pix
	default:
		return fmt.Errorf("image does not have an HDR color model")
	}
	info := Info{
		Header: Header{
			Size:              124,
			Flags:             HeaderFlagCaps | HeaderFlagHeight | HeaderFlagWidth | HeaderFlagPixelFormat | HeaderFlagMipMapCount,
			Width:             uint32(hdrImg.Bounds().Dx()),
			Height:            uint32(hdrImg.Bounds().Dy()),
			PitchOrLinearSize: 0,
			Depth:             0,
			MipMapCount:       1,
			Reserved:          [11]uint32{0},
			PixelFormat: PixelFormat{
				Size:        32,
				Flags:       PixelFormatFlagFourCC,
				FourCC:      [4]byte{'D', 'X', '1', '0'},
				RGBBitCount: 0,
				RBitMask:    0,
				GBitMask:    0,
				BBitMask:    0,
				ABitMask:    0,
			},
			Caps:      CapsFlag | CapsMipMap | CapsTexture,
			Caps2:     0,
			Caps3:     0,
			Caps4:     0,
			Reserved2: 0,
		},
		DXT10Header: &DXT10Header{
			DXGIFormat:        dxgiFmt,
			ResourceDimension: D3D10ResourceDimensionTexture2D,
			MiscFlag:          0,
			ArraySize:         1,
			MiscFlags2:        0,
		},
	}

	err := binary.Write(w, binary.LittleEndian, []byte("DDS "))
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, info.Header)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, info.DXT10Header)
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, pix)
	return err
}

func (d *DDS) dump(w io.Writer) error {
	d.Info.Header.MipMapCount = 1
	err := binary.Write(w, binary.LittleEndian, []byte("DDS "))
	if err != nil {
		return err
	}
	err = binary.Write(w, binary.LittleEndian, d.Info.Header)
	if err != nil {
		return err
	}
	if d.Info.DXT10Header != nil {
		err = binary.Write(w, binary.LittleEndian, *d.Info.DXT10Header)
		if err != nil {
			return err
		}
	}
	var pix []byte
	switch d.Info.ColorModel {
	case hdrColors.NRGBA64FModel:
		img, ok := d.Image.(*hdrColors.NRGBA64FImage)
		if !ok {
			return fmt.Errorf("failed to convert dds to NRGBA64F")
		}
		pix = img.Pix
	case hdrColors.NRGBA128FModel:
		img, ok := d.Image.(*hdrColors.NRGBA128FImage)
		if !ok {
			return fmt.Errorf("failed to convert dds to NRGBA128F")
		}
		pix = img.Pix
	case hdrColors.NRGBA128UModel:
		img, ok := d.Image.(*hdrColors.NRGBA128UImage)
		if !ok {
			return fmt.Errorf("failed to convert dds to NRGBA128U")
		}
		pix = img.Pix
	}
	err = binary.Write(w, binary.LittleEndian, pix)
	return err
}

// https://github.com/ImageMagick/ImageMagick/blob/main/coders/dds.c

func Decode(r io.Reader, readMipMaps bool) (*DDS, error) {
	info, err := DecodeInfo(r)
	if err != nil {
		return nil, err
	}

	mipMapsToRead := 1
	if readMipMaps || info.NumImages > 1 {
		mipMapsToRead = info.NumMipMaps
	}

	images := make([]*DDSImage, info.NumImages)
	for i := 0; i < info.NumImages; i++ {
		width, height := int(info.Header.Width), int(info.Header.Height)
		mipMaps := make([]*DDSMipMap, 0, mipMapsToRead)
		for j := 0; j < mipMapsToRead; j++ {
			if width == 0 || height == 0 {
				break
			}

			var buf []uint8
			var img image.Image
			switch info.ColorModel {
			case color.GrayModel:
				newImg := image.NewGray(image.Rect(0, 0, width, height))
				buf = newImg.Pix
				img = newImg
			case color.Gray16Model:
				newImg := image.NewGray16(image.Rect(0, 0, width, height))
				buf = newImg.Pix
				img = newImg
			case color.NRGBAModel:
				newImg := image.NewNRGBA(image.Rect(0, 0, width, height))
				buf = newImg.Pix
				img = newImg
			case color.NRGBA64Model:
				newImg := image.NewNRGBA64(image.Rect(0, 0, width, height))
				buf = newImg.Pix
				img = newImg
			case hdrColors.NRGBA64FModel:
				newImg := hdrColors.NewNRGBA64FImage(image.Rect(0, 0, width, height))
				buf = newImg.Pix
				img = newImg
			case hdrColors.NRGBA128FModel:
				newImg := hdrColors.NewNRGBA128FImage(image.Rect(0, 0, width, height))
				buf = newImg.Pix
				img = newImg
			default:
				return nil, errors.New("invalid color model passed by info structure")
			}
			if err := info.Decompress(buf, r, width, height, info); err != nil {
				return nil, err
			}
			mipMaps = append(mipMaps, &DDSMipMap{
				Image:  img,
				Width:  width,
				Height: height,
			})

			width /= 2
			height /= 2
		}

		if len(mipMaps) == 0 {
			return nil, errors.New("no mipmaps written")
		}

		images[i] = &DDSImage{
			Image:   mipMaps[0].Image,
			MipMaps: mipMaps,
		}
	}

	return &DDS{
		Image:  images[0].Image,
		Info:   info,
		Images: images,
	}, nil
}

func init() {
	image.RegisterFormat(
		"dds",
		"DDS ",
		func(r io.Reader) (image.Image, error) {
			return Decode(r, false)
		},
		DecodeConfig,
	)
}
