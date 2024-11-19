package ui

import (
	"context"
	"image"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/timebox"
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

// ScoreboardScreen displays the buttons for a 2 person scorekeeping system via a Timebox unit
type ScoreboardScreen struct {
	homeScreen deskpad.Screen

	redScore  int
	blueScore int

	tbc  *timebox.Conn
	keys []image.Image
}

// NewScoreboardScreen creates a new instance of the scoreboardscreen. Game starts at 0
func NewScoreboardScreen(tbc *timebox.Conn) *ScoreboardScreen {
	// Currently setup for a StreamDeck with 15 buttons
	sbs := &ScoreboardScreen{
		tbc:  tbc,
		keys: make([]image.Image, 15),
	}

	sbs.keys[scoreboardHomeKeyID] = loadAssetImage("assets/home-3-fill.png")
	sbs.keys[scoreboardRedPlusKeyID] = loadAssetImage("assets/add-line.png")
	sbs.keys[scoreboardRedIconKeyID] = loadAssetImage("assets/scoreboard-red.png")
	sbs.keys[scoreboardRedMinusKeyID] = loadAssetImage("assets/subtract-line.png")
	sbs.keys[scoreboardBluePlusKeyID] = loadAssetImage("assets/add-line.png")
	sbs.keys[scoreboardBlueIconKeyID] = loadAssetImage("assets/scoreboard-blue.png")
	sbs.keys[scoreboardBlueMinusKeyID] = loadAssetImage("assets/subtract-line.png")

	return sbs
}

// SetHomeScreen configures the screen navigated to when the 'Home' button is pressed
func (sbs *ScoreboardScreen) SetHomeScreen(screen deskpad.Screen) {
	sbs.homeScreen = screen
}

// Name is hardcoded to display as "scoreboard"
func (sbs *ScoreboardScreen) Name() string {
	return "scoreboard"
}

// Show returns the image set which will be shown to the user.
func (sbs *ScoreboardScreen) Show() []image.Image {
	sbs.redScore = 0
	sbs.blueScore = 0

	sbs.tbc.DisplayScoreboard(sbs.redScore, sbs.blueScore)
	return sbs.keys
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (sbs *ScoreboardScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		if id == scoreboardRedIconKeyID {
			sbs.redScore = 0
		} else if id == scoreboardBlueIconKeyID {
			sbs.blueScore = 0
		}
	}

	if id == scoreboardHomeKeyID {
		sbs.tbc.DisplayClock(true)

		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: sbs.homeScreen,
		}, nil
	} else if id == scoreboardRedPlusKeyID {
		sbs.redScore++
	} else if id == scoreboardRedMinusKeyID {
		sbs.redScore--
	} else if id == scoreboardBluePlusKeyID {
		sbs.blueScore++
	} else if id == scoreboardBlueMinusKeyID {
		sbs.blueScore--
	}

	sbs.tbc.DisplayScoreboard(sbs.redScore, sbs.blueScore)

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}
