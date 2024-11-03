package ui

import (
	"context"
	"errors"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/service"
)

const (
	mediaPlayerPrevKeyID        = 0
	mediaPlayerPlayPauseKeyID   = 1
	mediaPlayerNextKeyID        = 2
	mediaPlayerHomeKeyID        = 4
	mediaPlayerRewindKeyID      = 5
	mediaPlayerShuffleLoopKeyID = 6
	mediaPlayerFastForwardKeyID = 7
	mediaPlayerPlaylistKeyID    = 9
	mediaPlayerVolMuteKeyID     = 10
	mediaPlayerVolDownKeyID     = 11
	mediaPlayerVolUpKeyID       = 12
	mediaPlayerSettingsKeyID    = 14
)

// MediaPlayerScreen displays a control interface to the user which allows control of their media.
type MediaPlayerScreen struct {
	mpc service.MediaPlayerController

	homeScreen     deskpad.Screen
	playlistScreen deskpad.Screen
	settingsScreen deskpad.Screen

	playImg    image.Image
	pauseImg   image.Image
	shuffleImg image.Image
	loopImg    image.Image

	keys []image.Image
}

// NewMediaPlayerScreen creates a new screen for handling music playback, configured with the provided media player controller.
func NewMediaPlayerScreen(mpc service.MediaPlayerController) *MediaPlayerScreen {
	// Currently setup for a StreamDeck with 15 buttons
	mps := &MediaPlayerScreen{
		mpc:        mpc,
		playImg:    loadAssetImage("assets/play-fill.png"),
		pauseImg:   loadAssetImage("assets/pause-fill.png"),
		shuffleImg: loadAssetImage("assets/shuffle-fill.png"),
		loopImg:    loadAssetImage("assets/repeat-fill.png"),
		keys:       make([]image.Image, 15),
	}

	mps.keys[mediaPlayerPrevKeyID] = loadAssetImage("assets/skip-back-fill.png")
	mps.keys[mediaPlayerNextKeyID] = loadAssetImage("assets/skip-forward-fill.png")
	mps.keys[mediaPlayerHomeKeyID] = loadAssetImage("assets/home-3-fill.png")

	mps.keys[mediaPlayerRewindKeyID] = loadAssetImage("assets/replay-10-fill.png")
	mps.keys[mediaPlayerFastForwardKeyID] = loadAssetImage("assets/forward-10-fill.png")
	mps.keys[mediaPlayerPlaylistKeyID] = loadAssetImage("assets/folder-music-fill.png")

	mps.keys[mediaPlayerVolDownKeyID] = loadAssetImage("assets/volume-down-fill.png")
	mps.keys[mediaPlayerVolMuteKeyID] = loadAssetImage("assets/volume-mute-fill.png")
	mps.keys[mediaPlayerVolUpKeyID] = loadAssetImage("assets/volume-up-fill.png")
	mps.keys[mediaPlayerSettingsKeyID] = loadAssetImage("assets/settings-3-fill.png")

	return mps
}

// SetHomeScreen configures the screen navigated to when the 'Home' button is pressed
func (mps *MediaPlayerScreen) SetHomeScreen(screen deskpad.Screen) {
	mps.homeScreen = screen
}

// SetPlaylistScreen configures the screen navigated to when the 'Playlist' button is pressed
func (mps *MediaPlayerScreen) SetPlaylistScreen(screen deskpad.Screen) {
	mps.playlistScreen = screen
}

// SetSettingsScreen configures the screen navigated to when the 'Settings' button is pressed
func (mps *MediaPlayerScreen) SetSettingsScreen(screen deskpad.Screen) {
	mps.settingsScreen = screen
}

// Name is hardcoded to display as "media player"
func (mps *MediaPlayerScreen) Name() string {
	return "media player"
}

// Show returns the image set which will be shown to the user.
func (mps *MediaPlayerScreen) Show() []image.Image {
	if mps.mpc.IsPlaying() {
		mps.keys[mediaPlayerPlayPauseKeyID] = mps.pauseImg
	} else {
		mps.keys[mediaPlayerPlayPauseKeyID] = mps.playImg
	}

	if mps.mpc.IsShuffle() {
		mps.keys[mediaPlayerShuffleLoopKeyID] = mps.loopImg
	} else {
		mps.keys[mediaPlayerShuffleLoopKeyID] = mps.shuffleImg
	}

	return mps.keys
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (mps *MediaPlayerScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == mediaPlayerPrevKeyID {
		mps.mpc.Previous(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerNextKeyID {
		mps.mpc.Next(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerShuffleLoopKeyID {
		if mps.mpc.IsShuffle() {
			mps.mpc.Shuffle(ctx, false)
			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.loopImg,
			}, nil
		} else {
			mps.mpc.Shuffle(ctx, true)
			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.shuffleImg,
			}, nil
		}
	} else if id == mediaPlayerRewindKeyID {
		mps.mpc.Rewind(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerPlayPauseKeyID {
		if mps.mpc.IsPlaying() {
			mps.mpc.Pause(ctx)

			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.playImg,
			}, nil
		} else {
			mps.mpc.Play(ctx)
			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.pauseImg,
			}, nil
		}
	} else if id == mediaPlayerFastForwardKeyID {
		mps.mpc.FastForward(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerVolDownKeyID {
		mps.mpc.VolumeDown(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerVolMuteKeyID {
		if mps.mpc.IsMuted() {
			mps.mpc.Unmute(ctx)
		} else {
			mps.mpc.Mute(ctx)
		}
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerVolUpKeyID {
		mps.mpc.VolumeUp(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerSettingsKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.settingsScreen,
		}, nil
	} else if id == mediaPlayerHomeKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.homeScreen,
		}, nil
	} else if id == mediaPlayerPlaylistKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.playlistScreen,
		}, nil
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, errors.New("unhandled key")
}
