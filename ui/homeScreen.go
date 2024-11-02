package ui

import (
	"context"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
)

const (
	homePlayerScreenID   = 0
	homePlaylistScreenID = 1
)

// HomeScreen is the first screen shown to the user, listing all othe other possible screens
type HomeScreen struct {
	playerScreen   deskpad.Screen
	playlistScreen deskpad.Screen

	keys []image.Image
}

// NewHomeSCreen creates a home screen which allows navigation to the supplied screens.
func NewHomeScreen(player, playlist deskpad.Screen) *HomeScreen {
	// Currently setup for a StreamDeck with 15 buttons
	hs := &HomeScreen{
		playerScreen:   player,
		playlistScreen: playlist,
		keys:           make([]image.Image, 15),
	}

	hs.keys[homePlayerScreenID] = loadAssetImage("assets/music-2-fill.png")
	hs.keys[homePlaylistScreenID] = loadAssetImage("assets/folder-music-fill.png")

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

// KeyPressed handles the logic of what to do when a given key is pressed.
func (hs *HomeScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == homePlayerScreenID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: hs.playerScreen,
		}, nil
	} else if id == homePlaylistScreenID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: hs.playlistScreen,
		}, nil
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}
