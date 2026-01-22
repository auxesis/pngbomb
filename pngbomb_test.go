package main

import (
	"fmt"
	"image/png"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func newTempfile() *os.File {
	f, err := os.CreateTemp("", "test_image.*.png")
	if err != nil {
		log.Fatal(err)
	}
	return f
}

func TestGeneratePNGIsValid(t *testing.T) {
	var testCases = []struct {
		width  int
		height int
		file   *os.File
	}{
		{10, 10, newTempfile()},
		{100, 100, newTempfile()},
		{1000, 1000, newTempfile()},
		{10000, 10000, newTempfile()},
	}

	for _, tt := range testCases {
		t.Run(fmt.Sprintf("%dx%d", tt.width, tt.height), func(t *testing.T) {
			assert := assert.New(t)

			// generate the PNG
			err := generatePNG(tt.width, tt.height, tt.file)
			assert.NoError(err)

			// validate it can be decoded
			_, err = tt.file.Seek(0, 0)
			assert.NoError(err)
			img, err := png.Decode(tt.file)
			assert.NoError(err)
			assert.Equal(img.Bounds().Max.X, tt.width)
			assert.Equal(img.Bounds().Max.Y, tt.height)
		})
	}

}
