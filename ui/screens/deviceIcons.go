package screens

import (
	"image"
	"image/color"
	"image/draw"

	"github.com/rmrobinson/deskpad/ui"
)

var (
	colourOn    = color.RGBA{0x2e, 0xa0, 0x43, 0xff}
	colourOff   = color.RGBA{0x55, 0x55, 0x55, 0xff}
	colourGlyph = color.White
	// badgeBackground is the flat background every device-kind badge uses -
	// plain black, matching every other icon in the app (Home-menu icons,
	// NewRoomIcon, etc.) rather than a grey that stands out against them.
	badgeBackground = color.Black
)

func newBaseIcon(fill color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	if fill != nil {
		draw.Draw(img, img.Bounds(), image.NewUniform(fill), image.Point{}, draw.Src)
	}
	return img
}

func fillCircle(img *image.RGBA, cx, cy, r int, col color.Color) {
	r2 := r * r
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				img.Set(x, y, col)
			}
		}
	}
}

func strokeCircle(img *image.RGBA, cx, cy, r, thickness int, col color.Color) {
	fillCircle(img, cx, cy, r, col)
	fillCircle(img, cx, cy, r-thickness, img.At(0, 0))
}

func fillRect(img *image.RGBA, x0, y0, x1, y1 int, col color.Color) {
	draw.Draw(img, image.Rect(x0, y0, x1, y1), image.NewUniform(col), image.Point{}, draw.Src)
}

// overlayAsset draws the asset at assetPath over img at full opacity.
func overlayAsset(img *image.RGBA, assetPath string) *image.RGBA {
	return overlayAssetDimmed(img, assetPath, 0xff)
}

// overlayAssetDimmed draws the asset at assetPath over img, scaling the
// asset's own alpha by alpha/0xff first - used to knock a badge's icon back
// to a faint watermark so text drawn on top by NewTextIconWithBackground
// afterwards (white, full opacity) stays legible against it instead of
// fighting the glyph's own white linework.
func overlayAssetDimmed(img *image.RGBA, assetPath string, alpha uint8) *image.RGBA {
	glyph := loadAssetImage(assetPath)
	if glyph == nil {
		return img
	}
	if alpha == 0xff {
		draw.Draw(img, glyph.Bounds(), glyph, image.Point{}, draw.Over)
		return img
	}
	draw.DrawMask(img, glyph.Bounds(), glyph, image.Point{}, image.NewUniform(color.Alpha{alpha}), image.Point{}, draw.Over)
	return img
}

// badgeIconAlpha is how opaque a device-kind badge's icon is drawn at -
// dim enough that the device name text overlaid on top afterwards stays
// legible, bright enough that the icon is still clearly recognizable.
const badgeIconAlpha = 0x50

// NewKindBadge returns a background sized for a device button, illustrated
// by kind, suitable for passing to NewTextIconWithBackground. Every kind
// shares the same flat background - the icon itself is what distinguishes
// kind, not a per-kind tint.
func NewKindBadge(kind ui.DeviceKind) image.Image {
	switch kind {
	case ui.DeviceKindLight:
		return overlayAssetDimmed(newBaseIcon(badgeBackground), "assets/lightbulb-line.png", badgeIconAlpha)
	case ui.DeviceKindFan:
		return overlayAssetDimmed(newBaseIcon(badgeBackground), "assets/windy-line.png", badgeIconAlpha)
	case ui.DeviceKindOutlet:
		return overlayAssetDimmed(newBaseIcon(badgeBackground), "assets/outlet-2-line.png", badgeIconAlpha)
	case ui.DeviceKindStandingDesk:
		return overlayAssetDimmed(newBaseIcon(badgeBackground), "assets/cactus-line.png", badgeIconAlpha)
	default:
		return newBaseIcon(badgeBackground)
	}
}

// NewOnOffBadge returns a standalone icon for the on/off toggle button,
// coloured by its current state.
func NewOnOffBadge(on bool) image.Image {
	if on {
		return overlayAsset(newBaseIcon(colourOn), "assets/toggle-line.png")
	}
	return overlayAsset(newBaseIcon(colourOff), "assets/toggle-line.png")
}

// NewColourSwatch returns a solid-colour button used to set a light to a preset
// colour. selected draws a highlight ring when this swatch matches the device's
// current colour.
func NewColourSwatch(r, g, b int32, selected bool) image.Image {
	img := newBaseIcon(color.RGBA{uint8(r), uint8(g), uint8(b), 0xff})
	if selected {
		strokeCircle(img, width/2, height/2, 30, 3, colourGlyph)
	}
	return img
}

// NewStepIcon returns a stepper button for brightness or speed adjustment,
// illustrated by the asset at assetPath.
func NewStepIcon(assetPath string) image.Image {
	return overlayAsset(newBaseIcon(badgeBackground), assetPath)
}

// NewRoomIcon returns the house pictogram used for the device-list screen's
// own Home-menu icon. Background is plain black, matching every other
// Home-menu icon (bluetooth, media, etc.) - those are opaque-black assets
// baked in by their source files, so this needs its own black fill to match
// rather than standing out as a grey square among them.
func NewRoomIcon() image.Image {
	return overlayAsset(newBaseIcon(color.Black), "assets/home-gear-line.png")
}
