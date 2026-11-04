package screens

import (
	"context"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui/controllers"
)

const (
	homeClockID = 0
	homeTempID  = 1
)

// Home is the first screen shown to the user, listing all othe other possible screens
type Home struct {
	iconImg    image.Image
	keys       []image.Image
	controller HomeController

	screens []deskpad.Screen
}

// HomeController is an interface which defines what the home screen might control.
type HomeController interface {
	DisplayClock()
	DisplayTemperature()
	CurrentDisplay() controllers.HomeDisplay
}

// NewHome creates a home screen which allows navigation to the supplied screens.
func NewHome(hc HomeController) *Home {
	// Currently setup for a StreamDeck with 15 buttons
	hs := &Home{
		iconImg:    loadAssetImage("assets/home-3-fill.png"),
		keys:       make([]image.Image, 15),
		controller: hc,
		screens:    make([]deskpad.Screen, 15),
	}

	hs.screens[homeClockID] = hs
	hs.keys[homeClockID] = loadAssetImage("assets/time-line.png")
	hs.screens[homeTempID] = hs
	hs.keys[homeTempID] = loadAssetImage("assets/temp-cold-line.png")

	return hs
}

// Name is hardcoded to display as "home"
func (hs *Home) Name() string {
	return "home"
}

// Show returns the image set which will be shown to the user.
func (hs *Home) Show() []image.Image {
	if hs.controller.CurrentDisplay() == controllers.HomeDisplayClock {
		hs.controller.DisplayClock()
	} else if hs.controller.CurrentDisplay() == controllers.HomeDisplayTemperature {
		hs.controller.DisplayTemperature()
	}
	return hs.keys
}

// Icon returns the icon to display for this screen
func (hs *Home) Icon() image.Image {
	return hs.iconImg
}

// RegisterScreen adds a screen to the Home view in the next available spot
func (hs *Home) RegisterScreen(s deskpad.Screen) {
	for i, cs := range hs.screens {
		if cs == nil {
			hs.screens[i] = s
			hs.keys[i] = s.Icon()
			return
		}
	}
	log.Printf("home: no free slot to register screen %q; it will not be reachable from home\n", s.Name())
}

// Screens returns the navigable screens registered via RegisterScreen, in
// registration order. It excludes Home's own clock/temperature display
// slots, which are display modes rather than independently navigable
// screens.
func (hs *Home) Screens() []deskpad.Screen {
	out := make([]deskpad.Screen, 0, len(hs.screens))
	for _, s := range hs.screens {
		if s != nil && s != hs {
			out = append(out, s)
		}
	}
	return out
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (hs *Home) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == homeClockID {
		hs.controller.DisplayClock()

		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == homeTempID {
		hs.controller.DisplayTemperature()

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
