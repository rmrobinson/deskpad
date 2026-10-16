package screens

import (
	"image"
	"image/draw"
	"log"

	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"
	xfont "golang.org/x/image/font"
)

const (
	fontSize = 14
	// rowCharCount only decides how many characters go on each row (a
	// fixed-width split, not true word-wrap) - actual horizontal centering
	// uses glyphFace's real measured width instead, since this font isn't
	// monospace and a char-count estimate left short strings looking
	// left-aligned rather than centred.
	rowCharCount = 13
	width        = 72
	height       = width
)

var (
	font      *truetype.Font
	glyphFace xfont.Face
)

func init() {
	font = loadAssetFont("assets/m5x7.ttf")
	if font != nil {
		glyphFace = truetype.NewFace(font, &truetype.Options{Size: fontSize, DPI: 72})
	}
}

// NewTextIcon creates a new image from the supplied string which can be rendered onto a button in a legible fashion.
func NewTextIcon(input string) image.Image {
	return NewTextIconWithBackground(input, image.NewRGBA(image.Rect(0, 0, width, height)))
}

// NewTextIconWithBackground creates a new image which overlays the supplied text string over the supplied image.
func NewTextIconWithBackground(input string, bg image.Image) image.Image {
	freetypeCtx := freetype.NewContext()
	freetypeCtx.SetDPI(72)
	freetypeCtx.SetFont(font)
	freetypeCtx.SetFontSize(fontSize)
	freetypeCtx.SetClip(bg.Bounds())
	freetypeCtx.SetDst(bg.(draw.Image))
	freetypeCtx.SetSrc(image.NewUniform(image.White))

	charHeight := int(freetypeCtx.PointToFixed(fontSize) >> 6)

	if len(input) <= rowCharCount {
		row := freetype.Pt(getStartPoint(input), 15+charHeight)

		if _, err := freetypeCtx.DrawString(input, row); err != nil {
			log.Printf("unable to put string into img")
		}
	} else if len(input) <= rowCharCount*2 {
		inputRow1 := input[:rowCharCount]
		inputRow2 := input[rowCharCount:]

		row1 := freetype.Pt(1, 15+charHeight)
		row2 := freetype.Pt(getStartPoint(inputRow2), 33+charHeight)

		if _, err := freetypeCtx.DrawString(inputRow1, row1); err != nil {
			log.Printf("unable to put string into img")
		}
		if _, err := freetypeCtx.DrawString(inputRow2, row2); err != nil {
			log.Printf("unable to put string into img")
		}
	} else if len(input) <= rowCharCount*3 {
		inputRow1 := input[:rowCharCount]
		inputRow2 := input[rowCharCount : rowCharCount*2]
		inputRow3 := input[rowCharCount*2:]
		row1 := freetype.Pt(1, 7+charHeight)
		row2 := freetype.Pt(1, 22+charHeight)
		row3 := freetype.Pt(getStartPoint(inputRow3), 37+charHeight)

		if _, err := freetypeCtx.DrawString(inputRow1, row1); err != nil {
			log.Printf("unable to put string into img")
		}
		if _, err := freetypeCtx.DrawString(inputRow2, row2); err != nil {
			log.Printf("unable to put string into img")
		}
		if _, err := freetypeCtx.DrawString(inputRow3, row3); err != nil {
			log.Printf("unable to put string into img")
		}
	} else {
		inputRow1 := input[:rowCharCount]
		inputRow2 := input[rowCharCount : rowCharCount*2]
		inputRow3 := input[rowCharCount*2 : rowCharCount*3]
		inputRow4 := input[rowCharCount*3:]

		row1 := freetype.Pt(1, 2+charHeight)
		row2 := freetype.Pt(1, 17+charHeight)
		row3 := freetype.Pt(1, 32+charHeight)
		row4 := freetype.Pt(getStartPoint(inputRow4), 47+charHeight)

		if _, err := freetypeCtx.DrawString(inputRow1, row1); err != nil {
			log.Printf("unable to put string into img")
		}
		if _, err := freetypeCtx.DrawString(inputRow2, row2); err != nil {
			log.Printf("unable to put string into img")
		}
		if _, err := freetypeCtx.DrawString(inputRow3, row3); err != nil {
			log.Printf("unable to put string into img")
		}
		if _, err := freetypeCtx.DrawString(inputRow4, row4); err != nil {
			log.Printf("unable to put string into img")
		}
	}

	return bg
}

// getStartPoint returns the x pixel to start drawing input at so it's
// horizontally centred, based on its actual rendered width rather than a
// per-character estimate (this font isn't monospace - "i" and "W" don't
// take the same width, so a char-count guess left longer strings looking
// left-aligned instead of centred).
func getStartPoint(input string) int {
	if glyphFace == nil {
		return 1
	}

	textWidth := xfont.MeasureString(glyphFace, input).Round()
	start := (width - textWidth) / 2
	if start < 1 {
		start = 1
	}
	return start
}
