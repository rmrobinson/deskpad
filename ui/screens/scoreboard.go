package screens

import (
	"context"
	"image"

	"github.com/rmrobinson/deskpad"
)

const (
	scoreboardHomeKeyID      = 4
	scoreboardRedPlusKeyID   = 1
	scoreboardRedIconKeyID   = 6
	scoreboardRedMinusKeyID  = 11
	scoreboardBluePlusKeyID  = 3
	scoreboardBlueIconKeyID  = 8
	scoreboardBlueMinusKeyID = 13
)

// Scoreboard displays the buttons for a 2 person scorekeeping system via a Timebox unit
type Scoreboard struct {
	iconImg    image.Image
	keys       []image.Image
	controller ScoreboardController

	homeScreen deskpad.Screen
}

type ScoreboardController interface {
	Display()

	IncrementRedScore()
	DecrementRedScore()
	ResetRedScore()

	IncrementBlueScore()
	DecrementBlueScore()
	ResetBlueScore()
}

// NewScoreboard creates a new instance of the Scoreboard. Game starts at 0
func NewScoreboard(homeScreen *Home, sc ScoreboardController) *Scoreboard {
	// Currently setup for a StreamDeck with 15 buttons
	sbs := &Scoreboard{
		iconImg:    loadAssetImage("assets/group-3-line.png"),
		keys:       make([]image.Image, 15),
		controller: sc,
		homeScreen: homeScreen,
	}

	sbs.keys[scoreboardHomeKeyID] = homeScreen.Icon()
	sbs.keys[scoreboardRedPlusKeyID] = loadAssetImage("assets/add-line.png")
	sbs.keys[scoreboardRedIconKeyID] = loadAssetImage("assets/scoreboard-red.png")
	sbs.keys[scoreboardRedMinusKeyID] = loadAssetImage("assets/subtract-line.png")
	sbs.keys[scoreboardBluePlusKeyID] = loadAssetImage("assets/add-line.png")
	sbs.keys[scoreboardBlueIconKeyID] = loadAssetImage("assets/scoreboard-blue.png")
	sbs.keys[scoreboardBlueMinusKeyID] = loadAssetImage("assets/subtract-line.png")

	homeScreen.RegisterScreen(sbs)

	return sbs
}

// Name is hardcoded to display as "scoreboard"
func (sbs *Scoreboard) Name() string {
	return "scoreboard"
}

// Icon returns the icon to display for this screen
func (sbs *Scoreboard) Icon() image.Image {
	return sbs.iconImg
}

// Show returns the image set which will be shown to the user.
func (sbs *Scoreboard) Show() []image.Image {
	sbs.controller.Display()
	return sbs.keys
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (sbs *Scoreboard) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		if id == scoreboardRedIconKeyID {
			sbs.controller.ResetRedScore()
		} else if id == scoreboardBlueIconKeyID {
			sbs.controller.ResetBlueScore()
		}
	}

	if id == scoreboardHomeKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: sbs.homeScreen,
		}, nil
	} else if id == scoreboardRedPlusKeyID {
		sbs.controller.IncrementRedScore()
	} else if id == scoreboardRedMinusKeyID {
		sbs.controller.DecrementRedScore()
	} else if id == scoreboardBluePlusKeyID {
		sbs.controller.IncrementBlueScore()
	} else if id == scoreboardBlueMinusKeyID {
		sbs.controller.DecrementBlueScore()
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}
