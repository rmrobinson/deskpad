package ui

import (
	"bufio"
	"embed"
	"image"
	"io"
	"log"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
)

//go:embed assets
var assets embed.FS

func loadAssetImage(filePath string) image.Image {
	f, err := assets.Open(filePath)
	if err != nil {
		log.Printf("unable to open %s: %s\n", filePath, err.Error())
		return nil
	}
	defer f.Close()

	i, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		log.Printf("unable to decode image from %s: %s\n", filePath, err.Error())
		return nil
	}

	return i
}

func loadAssetFont(filePath string) *truetype.Font {
	f, err := assets.Open(filePath)
	if err != nil {
		log.Printf("unable to open %s: %s\n", filePath, err.Error())
		return nil
	}
	defer f.Close()

	bytes, err := io.ReadAll(f)
	if err != nil {
		log.Printf("unable to read bytes from font asset %s: %s\n", filePath, err.Error())
		return nil
	}

	font, err := freetype.ParseFont(bytes)
	if err != nil {
		log.Printf("unable to parse font from font asset %s: %s\n", filePath, err.Error())
		return nil
	}

	return font
}
