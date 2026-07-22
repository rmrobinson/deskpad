package screens

import (
	"context"
	"image"
	"image/color"
	"testing"

	"github.com/rmrobinson/deskpad"
)

type mediaPlayerTestController struct {
	playing bool
	shuffle bool
	muted   bool
}

func (c *mediaPlayerTestController) Play() {
	c.playing = true
}

func (c *mediaPlayerTestController) Pause() {
	c.playing = false
}

func (c *mediaPlayerTestController) Next() {}

func (c *mediaPlayerTestController) Previous() {}

func (c *mediaPlayerTestController) FastForward() {}

func (c *mediaPlayerTestController) Rewind() {}

func (c *mediaPlayerTestController) VolumeUp() {}

func (c *mediaPlayerTestController) VolumeDown() {}

func (c *mediaPlayerTestController) Mute() {
	c.muted = true
}

func (c *mediaPlayerTestController) Unmute() {
	c.muted = false
}

func (c *mediaPlayerTestController) Shuffle(shuffle bool) {
	c.shuffle = shuffle
}

func (c *mediaPlayerTestController) IsPlaying() bool {
	return c.playing
}

func (c *mediaPlayerTestController) IsShuffle() bool {
	return c.shuffle
}

func (c *mediaPlayerTestController) IsMuted() bool {
	return c.muted
}

func TestMediaPlayerPlayPauseUpdatesCachedKeyIcon(t *testing.T) {
	playImg := mediaPlayerTestImage(color.RGBA{R: 255, A: 255})
	pauseImg := mediaPlayerTestImage(color.RGBA{G: 255, A: 255})
	controller := &mediaPlayerTestController{}
	screen := &MediaPlayer{
		keys:       make([]image.Image, 15),
		controller: controller,
		playImg:    playImg,
		pauseImg:   pauseImg,
	}

	action, err := screen.KeyPressed(context.Background(), mediaPlayerPlayPauseKeyID, deskpad.KeyPressShort)
	if err != nil {
		t.Fatalf("KeyPressed returned error: %s", err)
	}
	if action.Action != deskpad.KeyPressActionUpdateIcon {
		t.Fatalf("action = %d, want update icon", action.Action)
	}
	if action.NewIcon != pauseImg {
		t.Fatalf("new icon after play was not pause image")
	}
	if screen.keys[mediaPlayerPlayPauseKeyID] != pauseImg {
		t.Fatalf("cached key after play was not pause image")
	}

	action, err = screen.KeyPressed(context.Background(), mediaPlayerPlayPauseKeyID, deskpad.KeyPressShort)
	if err != nil {
		t.Fatalf("KeyPressed returned error: %s", err)
	}
	if action.NewIcon != playImg {
		t.Fatalf("new icon after pause was not play image")
	}
	if screen.keys[mediaPlayerPlayPauseKeyID] != playImg {
		t.Fatalf("cached key after pause was not play image")
	}
}

func mediaPlayerTestImage(c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, c)
	return img
}
