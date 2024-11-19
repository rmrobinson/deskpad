package ui

import (
	"context"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/timebox"
)

const (
	homeClockID = 0
	homeTempID  = 1
)

// HomeScreen is the first screen shown to the user, listing all othe other possible screens
type HomeScreen struct {
	screens []deskpad.Screen
	keys    []image.Image

	iconImg image.Image
	tbc     *timebox.Conn
}

// NewHomeSCreen creates a home screen which allows navigation to the supplied screens.
func NewHomeScreen(tbc *timebox.Conn) *HomeScreen {
	// Currently setup for a StreamDeck with 15 buttons
	hs := &HomeScreen{
		screens: make([]deskpad.Screen, 15),
		keys:    make([]image.Image, 15),
		iconImg: loadAssetImage("assets/home-3-fill.png"),
		tbc:     tbc,
	}

	if tbc != nil {
		hs.keys[homeClockID] = loadAssetImage("assets/time-line.png")
		hs.keys[homeTempID] = loadAssetImage("assets/temp-cold-line.png")
	}

	return hs
}

// Name is hardcoded to display as "home"
func (hs *HomeScreen) Name() string {
	return "home"
}

// Show returns the image set which will be shown to the user.
func (hs *HomeScreen) Show() []image.Image {
	return hs.keys
}

// Icon returns the icon to display for this screen
func (hs *HomeScreen) Icon() image.Image {
	return hs.iconImg
}

// RegisterScreen adds a screen to the Home view in the next available spot
func (hs *HomeScreen) RegisterScreen(s deskpad.Screen) {
	for i, cs := range hs.screens {
		if cs == nil {
			hs.screens[i] = s
			hs.keys[i] = s.Icon()
			return
		}
	}
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (hs *HomeScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == homeClockID && hs.tbc != nil {
		hs.tbc.DisplayClock(true)

		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == homeTempID && hs.tbc != nil {
		hs.tbc.DisplayTemperature(true)

		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	}

	if s := hs.screens[id]; s != nil {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: s,
		}, nil
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}
