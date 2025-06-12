package hdrColors

import (
	"encoding/binary"
	"image"
	"image/color"
	"math/bits"

	"github.com/x448/float16"
)

type GraySetting int32

const (
	GraySettingNone    GraySetting = 0
	GraySettingRed     GraySetting = 1
	GraySettingGreen   GraySetting = 2
	GraySettingBlue    GraySetting = 3
	GraySettingAlpha   GraySetting = 4
	GraySettingNoAlpha GraySetting = 5
)

type NRGBA128F struct {
	R, G, B, A float32
}

func (c NRGBA128F) RGBA() (r, g, b, a uint32) {
	rClip := min(max(c.R, 0.0), 1.0)
	gClip := min(max(c.G, 0.0), 1.0)
	bClip := min(max(c.B, 0.0), 1.0)
	aClip := min(max(c.A, 0.0), 1.0)

	rMult := rClip * aClip
	gMult := gClip * aClip
	bMult := bClip * aClip

	r = uint32(rMult * 65535.0)
	g = uint32(gMult * 65535.0)
	b = uint32(bMult * 65535.0)
	a = uint32(aClip * 65535.0)
	return
}

func (c *NRGBA128F) Channel(ch string) *float32 {
	switch ch {
	case "R":
		return &c.R
	case "B":
		return &c.B
	case "G":
		return &c.G
	case "A":
		return &c.A
	}
	return nil
}

type NRGBA64F struct {
	R, G, B, A float16.Float16
}

func (c NRGBA64F) RGBA() (r, g, b, a uint32) {
	rClip := min(max(c.R.Float32(), 0.0), 1.0)
	gClip := min(max(c.G.Float32(), 0.0), 1.0)
	bClip := min(max(c.B.Float32(), 0.0), 1.0)
	aClip := min(max(c.A.Float32(), 0.0), 1.0)

	rMult := rClip * aClip
	gMult := gClip * aClip
	bMult := bClip * aClip

	r = uint32(rMult * 65535.0)
	g = uint32(gMult * 65535.0)
	b = uint32(bMult * 65535.0)
	a = uint32(aClip * 65535.0)
	return
}

func (c *NRGBA64F) Channel(ch string) *float16.Float16 {
	switch ch {
	case "R":
		return &c.R
	case "B":
		return &c.B
	case "G":
		return &c.G
	case "A":
		return &c.A
	}
	return nil
}

type NRGBA128U struct {
	R, G, B, A uint32
}

func (c NRGBA128U) RGBA() (r, g, b, a uint32) {
	rClip := float32(c.R) / 4294967295.0
	gClip := float32(c.G) / 4294967295.0
	bClip := float32(c.B) / 4294967295.0
	aClip := float32(c.A) / 4294967295.0

	rMult := rClip * aClip
	gMult := gClip * aClip
	bMult := bClip * aClip

	r = uint32(rMult * 65535.0)
	g = uint32(gMult * 65535.0)
	b = uint32(bMult * 65535.0)
	a = uint32(aClip * 65535.0)
	return
}

func (c *NRGBA128U) Channel(ch string) *uint32 {
	switch ch {
	case "R":
		return &c.R
	case "B":
		return &c.B
	case "G":
		return &c.G
	case "A":
		return &c.A
	}
	return nil
}

// HDR Color Models
var (
	NRGBA128UModel color.Model = color.ModelFunc(nrgba128UModel)
	NRGBA64FModel  color.Model = color.ModelFunc(nrgba64FModel)
	NRGBA128FModel color.Model = color.ModelFunc(nrgba128FModel)
)

func nrgba128UModel(c color.Color) color.Color {
	r, g, b, a := c.RGBA()

	rMult := float32(r) / 65535.0
	gMult := float32(g) / 65535.0
	bMult := float32(b) / 65535.0
	a32f := float32(a) / 65535.0

	var (
		r32f float32 = 0.0
		g32f float32 = 0.0
		b32f float32 = 0.0
	)

	if a32f > 0.0 {
		r32f = rMult / a32f
		g32f = gMult / a32f
		b32f = bMult / a32f
	}

	return NRGBA128U{
		R: uint32(r32f * 4294967295.0),
		G: uint32(g32f * 4294967295.0),
		B: uint32(b32f * 4294967295.0),
		A: uint32(a32f * 4294967295.0),
	}
}

func nrgba64FModel(c color.Color) color.Color {
	if c16, ok := c.(NRGBA64F); ok {
		return c16
	} else if c32, ok := c.(NRGBA128F); ok {
		return NRGBA64F{
			R: float16.Fromfloat32(c32.R),
			G: float16.Fromfloat32(c32.G),
			B: float16.Fromfloat32(c32.B),
			A: float16.Fromfloat32(c32.A),
		}
	}

	r, g, b, a := c.RGBA()

	rMult := float32(r) / 65535.0
	gMult := float32(g) / 65535.0
	bMult := float32(b) / 65535.0
	a32f := float32(a) / 65535.0

	var (
		r32f float32 = 0.0
		g32f float32 = 0.0
		b32f float32 = 0.0
	)

	if a32f > 0.0 {
		r32f = rMult / a32f
		g32f = gMult / a32f
		b32f = bMult / a32f
	}

	return NRGBA64F{
		R: float16.Fromfloat32(r32f),
		G: float16.Fromfloat32(g32f),
		B: float16.Fromfloat32(b32f),
		A: float16.Fromfloat32(a32f),
	}
}

func nrgba128FModel(c color.Color) color.Color {
	if c16, ok := c.(NRGBA64F); ok {
		return NRGBA128F{
			R: c16.R.Float32(),
			G: c16.G.Float32(),
			B: c16.B.Float32(),
			A: c16.A.Float32(),
		}
	} else if c32, ok := c.(NRGBA128F); ok {
		return c32
	}

	r, g, b, a := c.RGBA()

	rMult := float32(r) / 65535.0
	gMult := float32(g) / 65535.0
	bMult := float32(b) / 65535.0
	a32f := float32(a) / 65535.0

	var (
		r32f float32 = 0.0
		g32f float32 = 0.0
		b32f float32 = 0.0
	)

	if a32f > 0.0 {
		r32f = rMult / a32f
		g32f = gMult / a32f
		b32f = bMult / a32f
	}

	return NRGBA128F{
		R: r32f,
		G: g32f,
		B: b32f,
		A: a32f,
	}
}

const (
	nrgba128UPixelBytes = 16
	nrgba128FPixelBytes = 16
	nrgba64FPixelBytes  = 8
)

type Grayable interface {
	SetGray(gray GraySetting)
}

// NRGBA128FImage is an in-memory image whose At method returns [openexr.RGBA32F] values.
type NRGBA128FImage struct {
	// Pix holds the image's pixels, in R, G, B, A order and big-endian format. The pixel at
	// (x, y) starts at Pix[(y-Rect.Min.Y)*Stride + (x-Rect.Min.X)*16].
	Pix []uint8
	// Stride is the Pix stride (in bytes) between vertically adjacent pixels.
	Stride int
	// Rect is the image's bounds.
	Rect image.Rectangle
	// Grayscale mode
	Grayscale GraySetting
}

func (p *NRGBA128FImage) ColorModel() color.Model { return NRGBA128FModel }

func (p *NRGBA128FImage) Bounds() image.Rectangle { return p.Rect }

func (p *NRGBA128FImage) At(x, y int) color.Color {
	return p.NRGBA128FAt(x, y)
}

func (p *NRGBA128FImage) RGBA64At(x, y int) color.RGBA64 {
	r, g, b, a := p.NRGBA128FAt(x, y).RGBA()
	return color.RGBA64{uint16(r), uint16(g), uint16(b), uint16(a)}
}

func (p *NRGBA128FImage) NRGBA128FAt(x, y int) NRGBA128F {
	if !(image.Point{x, y}.In(p.Rect)) {
		return NRGBA128F{}
	}
	i := p.PixOffset(x, y)
	s := p.Pix[i : i+nrgba128FPixelBytes : i+nrgba128FPixelBytes] // Small cap improves performance, see https://golang.org/issue/27857
	var r, g, b, a float32
	binary.Decode(s, binary.LittleEndian, &r)
	binary.Decode(s[4:], binary.LittleEndian, &g)
	binary.Decode(s[8:], binary.LittleEndian, &b)
	binary.Decode(s[12:], binary.LittleEndian, &a)
	switch p.Grayscale {
	case GraySettingRed:
		g, b = r, r
		a = 1.0
	case GraySettingGreen:
		r, b = g, g
		a = 1.0
	case GraySettingBlue:
		r, g = b, b
		a = 1.0
	case GraySettingAlpha:
		r, g, b = a, a, a
		a = 1.0
	case GraySettingNoAlpha:
		a = 1.0
	}
	return NRGBA128F{
		R: r,
		G: g,
		B: b,
		A: a,
	}
}

func (p *NRGBA128FImage) SetGray(gray GraySetting) {
	p.Grayscale = gray
}

// PixOffset returns the index of the first element of Pix that corresponds to
// the pixel at (x, y).
func (p *NRGBA128FImage) PixOffset(x, y int) int {
	return (y-p.Rect.Min.Y)*p.Stride + (x-p.Rect.Min.X)*nrgba128FPixelBytes
}

func (p *NRGBA128FImage) Set(x, y int, c color.Color) {
	if !(image.Point{x, y}.In(p.Rect)) {
		return
	}
	i := p.PixOffset(x, y)
	c1, ok := c.(NRGBA128F)
	if !ok {
		c1 = NRGBA128FModel.Convert(c).(NRGBA128F)
	}
	s := p.Pix[i : i+nrgba128FPixelBytes : i+nrgba128FPixelBytes] // Small cap improves performance, see https://golang.org/issue/27857

	binary.Encode(s, binary.LittleEndian, c1.R)
	binary.Encode(s[4:], binary.LittleEndian, c1.G)
	binary.Encode(s[8:], binary.LittleEndian, c1.B)
	binary.Encode(s[12:], binary.LittleEndian, c1.A)
}

// SubImage returns an image representing the portion of the image p visible
// through r. The returned value shares pixels with the original image.
func (p *NRGBA128FImage) SubImage(r image.Rectangle) image.Image {
	r = r.Intersect(p.Rect)
	// If r1 and r2 are Rectangles, r1.Intersect(r2) is not guaranteed to be inside
	// either r1 or r2 if the intersection is empty. Without explicitly checking for
	// this, the Pix[i:] expression below can panic.
	if r.Empty() {
		return &NRGBA128FImage{}
	}
	i := p.PixOffset(r.Min.X, r.Min.Y)
	return &NRGBA128FImage{
		Pix:       p.Pix[i:],
		Stride:    p.Stride,
		Rect:      r,
		Grayscale: p.Grayscale,
	}
}

// Opaque scans the entire image and reports whether it is fully opaque.
func (p *NRGBA128FImage) Opaque() bool {
	if p.Rect.Empty() {
		return true
	}
	i0, i1 := 12, p.Rect.Dx()*nrgba128FPixelBytes
	for y := p.Rect.Min.Y; y < p.Rect.Max.Y; y++ {
		for i := i0; i < i1; i += nrgba128FPixelBytes {
			var alpha float32
			binary.Decode(p.Pix[i:], binary.LittleEndian, alpha)
			if alpha < 1.0 {
				return false
			}
		}
		i0 += p.Stride
		i1 += p.Stride
	}
	return true
}

// NewNRGBA128FImage returns a new [NRGBA128FImage] image with the given bounds.
func NewNRGBA128FImage(r image.Rectangle) *NRGBA128FImage {
	return &NRGBA128FImage{
		Pix:       make([]uint8, pixelBufferLength(16, r, "NRGBA128FImage")),
		Stride:    nrgba128FPixelBytes * r.Dx(),
		Rect:      r,
		Grayscale: GraySettingNone,
	}
}

// NRGBA64FImage is an in-memory image whose At method returns [openexr.RGBA16F] values.
type NRGBA64FImage struct {
	// Pix holds the image's pixels, in R, G, B, A order and little-endian format. The pixel at
	// (x, y) starts at Pix[(y-Rect.Min.Y)*Stride + (x-Rect.Min.X)*8].
	Pix []uint8
	// Stride is the Pix stride (in bytes) between vertically adjacent pixels.
	Stride int
	// Rect is the image's bounds.
	Rect image.Rectangle
	// Grayscale mode
	Grayscale GraySetting
}

func (p *NRGBA64FImage) ColorModel() color.Model { return NRGBA64FModel }

func (p *NRGBA64FImage) Bounds() image.Rectangle { return p.Rect }

func (p *NRGBA64FImage) At(x, y int) color.Color {
	return p.NRGBA64FAt(x, y)
}

func (p *NRGBA64FImage) RGBA64At(x, y int) color.RGBA64 {
	r, g, b, a := p.NRGBA64FAt(x, y).RGBA()
	return color.RGBA64{uint16(r), uint16(g), uint16(b), uint16(a)}
}

func (p *NRGBA64FImage) NRGBA64FAt(x, y int) NRGBA64F {
	if !(image.Point{x, y}.In(p.Rect)) {
		return NRGBA64F{}
	}
	i := p.PixOffset(x, y)
	s := p.Pix[i : i+nrgba64FPixelBytes : i+nrgba64FPixelBytes] // Small cap improves performance, see https://golang.org/issue/27857
	var r, g, b, a float16.Float16
	binary.Decode(s, binary.LittleEndian, &r)
	binary.Decode(s[2:], binary.LittleEndian, &g)
	binary.Decode(s[4:], binary.LittleEndian, &b)
	binary.Decode(s[6:], binary.LittleEndian, &a)
	switch p.Grayscale {
	case GraySettingRed:
		g, b = r, r
		a = float16.Fromfloat32(1.0)
	case GraySettingGreen:
		r, b = g, g
		a = float16.Fromfloat32(1.0)
	case GraySettingBlue:
		r, g = b, b
		a = float16.Fromfloat32(1.0)
	case GraySettingAlpha:
		r, g, b = a, a, a
		a = float16.Fromfloat32(1.0)
	case GraySettingNoAlpha:
		a = float16.Fromfloat32(1.0)
	}
	return NRGBA64F{
		R: r,
		G: g,
		B: b,
		A: a,
	}
}

func (p *NRGBA64FImage) SetGray(gray GraySetting) {
	p.Grayscale = gray
}

// PixOffset returns the index of the first element of Pix that corresponds to
// the pixel at (x, y).
func (p *NRGBA64FImage) PixOffset(x, y int) int {
	return (y-p.Rect.Min.Y)*p.Stride + (x-p.Rect.Min.X)*nrgba64FPixelBytes
}

func (p *NRGBA64FImage) Set(x, y int, c color.Color) {
	if !(image.Point{x, y}.In(p.Rect)) {
		return
	}
	i := p.PixOffset(x, y)
	c1, ok := c.(NRGBA64F)
	if !ok {
		c1 = NRGBA64FModel.Convert(c).(NRGBA64F)
	}
	s := p.Pix[i : i+nrgba64FPixelBytes : i+nrgba64FPixelBytes] // Small cap improves performance, see https://golang.org/issue/27857

	binary.Encode(s, binary.LittleEndian, c1.R)
	binary.Encode(s[2:], binary.LittleEndian, c1.G)
	binary.Encode(s[4:], binary.LittleEndian, c1.B)
	binary.Encode(s[6:], binary.LittleEndian, c1.A)
}

// SubImage returns an image representing the portion of the image p visible
// through r. The returned value shares pixels with the original image.
func (p *NRGBA64FImage) SubImage(r image.Rectangle) image.Image {
	r = r.Intersect(p.Rect)
	// If r1 and r2 are Rectangles, r1.Intersect(r2) is not guaranteed to be inside
	// either r1 or r2 if the intersection is empty. Without explicitly checking for
	// this, the Pix[i:] expression below can panic.
	if r.Empty() {
		return &NRGBA64FImage{}
	}
	i := p.PixOffset(r.Min.X, r.Min.Y)
	return &NRGBA64FImage{
		Pix:    p.Pix[i:],
		Stride: p.Stride,
		Rect:   r,
	}
}

// Opaque scans the entire image and reports whether it is fully opaque.
func (p *NRGBA64FImage) Opaque() bool {
	if p.Rect.Empty() {
		return true
	}
	i0, i1 := 12, p.Rect.Dx()*nrgba64FPixelBytes
	for y := p.Rect.Min.Y; y < p.Rect.Max.Y; y++ {
		for i := i0; i < i1; i += nrgba64FPixelBytes {
			var alpha float32
			binary.Decode(p.Pix[i:], binary.LittleEndian, alpha)
			if alpha < 1.0 {
				return false
			}
		}
		i0 += p.Stride
		i1 += p.Stride
	}
	return true
}

// NewNRGBA64FImage returns a new [NRGBA64FImage] image with the given bounds.
func NewNRGBA64FImage(r image.Rectangle) *NRGBA64FImage {
	return &NRGBA64FImage{
		Pix:       make([]uint8, pixelBufferLength(nrgba64FPixelBytes, r, "NRGBA64FImage")),
		Stride:    nrgba64FPixelBytes * r.Dx(),
		Rect:      r,
		Grayscale: GraySettingNone,
	}
}

// NRGBA128UImage is an in-memory image whose At method returns [openexr.NRGBA128] values.
type NRGBA128UImage struct {
	// Pix holds the image's pixels, in R, G, B, A order and little-endian format. The pixel at
	// (x, y) starts at Pix[(y-Rect.Min.Y)*Stride + (x-Rect.Min.X)*8].
	Pix []uint8
	// Stride is the Pix stride (in bytes) between vertically adjacent pixels.
	Stride int
	// Rect is the image's bounds.
	Rect image.Rectangle
	// Grayscale mode
	Grayscale GraySetting
}

func (p *NRGBA128UImage) ColorModel() color.Model { return NRGBA128UModel }

func (p *NRGBA128UImage) Bounds() image.Rectangle { return p.Rect }

func (p *NRGBA128UImage) At(x, y int) color.Color {
	return p.NRGBA128UAt(x, y)
}

func (p *NRGBA128UImage) RGBA64At(x, y int) color.RGBA64 {
	r, g, b, a := p.NRGBA128UAt(x, y).RGBA()
	return color.RGBA64{uint16(r), uint16(g), uint16(b), uint16(a)}
}

func (p *NRGBA128UImage) NRGBA128UAt(x, y int) NRGBA128U {
	if !(image.Point{x, y}.In(p.Rect)) {
		return NRGBA128U{}
	}
	i := p.PixOffset(x, y)
	s := p.Pix[i : i+nrgba128UPixelBytes : i+nrgba128UPixelBytes] // Small cap improves performance, see https://golang.org/issue/27857
	var r, g, b, a uint32
	binary.Decode(s, binary.LittleEndian, &r)
	binary.Decode(s[4:], binary.LittleEndian, &g)
	binary.Decode(s[8:], binary.LittleEndian, &b)
	binary.Decode(s[12:], binary.LittleEndian, &a)
	switch p.Grayscale {
	case GraySettingRed:
		g, b = r, r
		a = 0xffffffff
	case GraySettingGreen:
		r, b = g, g
		a = 0xffffffff
	case GraySettingBlue:
		r, g = b, b
		a = 0xffffffff
	case GraySettingAlpha:
		r, g, b = a, a, a
		a = 0xffffffff
	case GraySettingNoAlpha:
		a = 0xffffffff
	}
	return NRGBA128U{
		R: r,
		G: g,
		B: b,
		A: a,
	}
}

func (p *NRGBA128UImage) SetGray(gray GraySetting) {
	p.Grayscale = gray
}

// PixOffset returns the index of the first element of Pix that corresponds to
// the pixel at (x, y).
func (p *NRGBA128UImage) PixOffset(x, y int) int {
	return (y-p.Rect.Min.Y)*p.Stride + (x-p.Rect.Min.X)*nrgba128UPixelBytes
}

func (p *NRGBA128UImage) Set(x, y int, c color.Color) {
	if !(image.Point{x, y}.In(p.Rect)) {
		return
	}
	i := p.PixOffset(x, y)
	c1, ok := c.(NRGBA128U)
	if !ok {
		c1 = NRGBA128UModel.Convert(c).(NRGBA128U)
	}
	s := p.Pix[i : i+nrgba128UPixelBytes : i+nrgba128UPixelBytes] // Small cap improves performance, see https://golang.org/issue/27857

	binary.Encode(s, binary.LittleEndian, c1.R)
	binary.Encode(s[4:], binary.LittleEndian, c1.G)
	binary.Encode(s[8:], binary.LittleEndian, c1.B)
	binary.Encode(s[12:], binary.LittleEndian, c1.A)
}

// SubImage returns an image representing the portion of the image p visible
// through r. The returned value shares pixels with the original image.
func (p *NRGBA128UImage) SubImage(r image.Rectangle) image.Image {
	r = r.Intersect(p.Rect)
	// If r1 and r2 are Rectangles, r1.Intersect(r2) is not guaranteed to be inside
	// either r1 or r2 if the intersection is empty. Without explicitly checking for
	// this, the Pix[i:] expression below can panic.
	if r.Empty() {
		return &NRGBA128UImage{}
	}
	i := p.PixOffset(r.Min.X, r.Min.Y)
	return &NRGBA128UImage{
		Pix:       p.Pix[i:],
		Stride:    p.Stride,
		Rect:      r,
		Grayscale: p.Grayscale,
	}
}

// Opaque scans the entire image and reports whether it is fully opaque.
func (p *NRGBA128UImage) Opaque() bool {
	if p.Rect.Empty() {
		return true
	}
	i0, i1 := 12, p.Rect.Dx()*nrgba128UPixelBytes
	for y := p.Rect.Min.Y; y < p.Rect.Max.Y; y++ {
		for i := i0; i < i1; i += nrgba128UPixelBytes {
			var alpha float32
			binary.Decode(p.Pix[i:], binary.LittleEndian, alpha)
			if alpha < 1.0 {
				return false
			}
		}
		i0 += p.Stride
		i1 += p.Stride
	}
	return true
}

// NewNRGBA128Image returns a new [NRGBA128UImage] image with the given bounds.
func NewNRGBA128UImage(r image.Rectangle) *NRGBA128UImage {
	return &NRGBA128UImage{
		Pix:       make([]uint8, pixelBufferLength(nrgba128UPixelBytes, r, "NRGBA128UImage")),
		Stride:    nrgba128UPixelBytes * r.Dx(),
		Rect:      r,
		Grayscale: GraySettingNone,
	}
}

// pixelBufferLength returns the length of the []uint8 typed Pix slice field
// for the NewXxx functions. Conceptually, this is just (bpp * width * height),
// but this function panics if at least one of those is negative or if the
// computation would overflow the int type.
//
// This panics instead of returning an error because of backwards
// compatibility. The NewXxx functions do not return an error.
func pixelBufferLength(bytesPerPixel int, r image.Rectangle, imageTypeName string) int {
	totalLength := mul3NonNeg(bytesPerPixel, r.Dx(), r.Dy())
	if totalLength < 0 {
		panic("image: New" + imageTypeName + " Rectangle has huge or negative dimensions")
	}
	return totalLength
}

// mul3NonNeg returns (x * y * z), unless at least one argument is negative or
// if the computation overflows the int type, in which case it returns -1.
func mul3NonNeg(x int, y int, z int) int {
	if (x < 0) || (y < 0) || (z < 0) {
		return -1
	}
	hi, lo := bits.Mul64(uint64(x), uint64(y))
	if hi != 0 {
		return -1
	}
	hi, lo = bits.Mul64(lo, uint64(z))
	if hi != 0 {
		return -1
	}
	a := int(lo)
	if (a < 0) || (uint64(a) != lo) {
		return -1
	}
	return a
}
