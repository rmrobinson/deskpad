package ui

import (
	"context"
	"image"
	"log"

	"github.com/disintegration/gift"
	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/service"
)

const (
	mediaPlaylistHomeKeyID   = 4
	mediaPlaylistPlayerKeyID = 9
	mediaPlaylistNextKeyID   = 14
)

// MediaPlaylistScreen displays a number of different media playlists to the user.
type MediaPlaylistScreen struct {
	mpc    service.MediaPlaylistController
	mplayc service.MediaPlayerController

	homeScreen   deskpad.Screen
	playerScreen deskpad.Screen

	iconImg image.Image

	playlists          []service.MediaPlaylist
	currPlaylistOffset int

	keys []image.Image
}

// NewMediaPlaylistScreen creates a new instance of the playlist screen, configured with the provided playlist controller.
func NewMediaPlaylistScreen(homeScreen *HomeScreen, mpc service.MediaPlaylistController, mplayc service.MediaPlayerController) *MediaPlaylistScreen {
	// Currently setup for a StreamDeck with 15 buttons
	mps := &MediaPlaylistScreen{
		mpc:                mpc,
		mplayc:             mplayc,
		homeScreen:         homeScreen,
		iconImg:            loadAssetImage("assets/folder-music-fill.png"),
		playlists:          []service.MediaPlaylist{},
		currPlaylistOffset: 0,
		keys:               make([]image.Image, 15),
	}

	mps.keys[mediaPlaylistHomeKeyID] = homeScreen.Icon()
	mps.keys[mediaPlaylistNextKeyID] = loadAssetImage("assets/skip-right-line.png")

	homeScreen.RegisterScreen(mps)

	return mps
}

// SetPlayerScreen configures the screen navigated to when the 'Player' button is pressed
func (mps *MediaPlaylistScreen) SetPlayerScreen(screen deskpad.Screen) {
	mps.playerScreen = screen
	mps.keys[mediaPlaylistPlayerKeyID] = screen.Icon()
}

// Name is hardcoded to display as "media playlist"
func (mps *MediaPlaylistScreen) Name() string {
	return "media playlist"
}

// Icon returns the icon to display for this screen
func (mps *MediaPlaylistScreen) Icon() image.Image {
	return mps.iconImg
}

// Show returns the image set which will be shown to the user.
func (mps *MediaPlaylistScreen) Show() []image.Image {
	mps.playlists = mps.mpc.GetPlaylists(12, mps.currPlaylistOffset)

	for playlistPos, playlist := range mps.playlists {
		mps.keys[playlistIdxToKeyID(playlistPos)] = resize(playlist.Icon)
	}

	for clearIdx := len(mps.playlists); clearIdx < 12; clearIdx++ {
		mps.keys[playlistIdxToKeyID(clearIdx)] = nil
	}

	return mps.keys
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (mps *MediaPlaylistScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == mediaPlaylistHomeKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.homeScreen,
		}, nil
	} else if id == mediaPlaylistPlayerKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.playerScreen,
		}, nil
	} else if id == mediaPlaylistNextKeyID {
		mps.currPlaylistOffset += 12
		// If we didn't get a full set, assume we're at the end and start over
		if len(mps.playlists) < 12 {
			mps.currPlaylistOffset = 0
		}

		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionRefreshScreen,
		}, nil
	}

	playlistIdx := keyIDToPlaylistIdx(id)
	mps.mplayc.StartPlaylist(ctx, mps.playlists[playlistIdx].ID)

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}

func keyIDToPlaylistIdx(id int) int {
	if id <= 3 {
		return id
	} else if id >= 5 && id <= 8 {
		return id - 1
	} else if id >= 10 && id <= 13 {
		return id - 2
	} else {
		return 0
	}
}

func playlistIdxToKeyID(pos int) int {
	keyID := pos
	if pos > 3 && pos <= 7 {
		keyID = pos + 1
	} else if pos > 7 && pos <= 11 {
		keyID = pos + 2
	}
	return keyID
}

func resize(img image.Image) image.Image {
	g := gift.New(
		gift.Resize(72, 72, gift.LanczosResampling),
		gift.UnsharpMask(1, 1, 0),
	)
	res := image.NewRGBA(g.Bounds(img.Bounds()))
	g.Draw(res, img)
	return res
}
