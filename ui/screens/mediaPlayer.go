package screens

import (
	"context"
	"errors"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
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

// MediaPlayer displays a control interface to the user which allows control of their media.
type MediaPlayer struct {
	iconImg    image.Image
	keys       []image.Image
	controller MediaPlayerController

	homeScreen     deskpad.Screen
	playlistScreen deskpad.Screen
	settingsScreen deskpad.Screen

	playImg    image.Image
	pauseImg   image.Image
	shuffleImg image.Image
	loopImg    image.Image
}

// MediaPlayerController describes the functions which the screen will use to allow the user to interface with the media source.
type MediaPlayerController interface {
	Play()
	Pause()
	Next()
	Previous()
	FastForward()
	Rewind()
	VolumeUp()
	VolumeDown()
	Mute()
	Unmute()
	Shuffle(shuffle bool)

	IsPlaying() bool
	IsShuffle() bool
	IsMuted() bool
}

// NewMediaPlayer creates a new screen for handling music playback, configured with the provided media player controller.
func NewMediaPlayer(homeScreen *Home, mpc MediaPlayerController) *MediaPlayer {
	// Currently setup for a StreamDeck with 15 buttons
	mps := &MediaPlayer{
		iconImg:    loadAssetImage("assets/music-2-fill.png"),
		keys:       make([]image.Image, 15),
		controller: mpc,
		homeScreen: homeScreen,
		playImg:    loadAssetImage("assets/play-fill.png"),
		pauseImg:   loadAssetImage("assets/pause-fill.png"),
		shuffleImg: loadAssetImage("assets/shuffle-fill.png"),
		loopImg:    loadAssetImage("assets/repeat-fill.png"),
	}

	mps.keys[mediaPlayerHomeKeyID] = homeScreen.Icon()
	mps.keys[mediaPlayerPrevKeyID] = loadAssetImage("assets/skip-back-fill.png")
	mps.keys[mediaPlayerNextKeyID] = loadAssetImage("assets/skip-forward-fill.png")
	mps.keys[mediaPlayerRewindKeyID] = loadAssetImage("assets/replay-10-fill.png")
	mps.keys[mediaPlayerFastForwardKeyID] = loadAssetImage("assets/forward-10-fill.png")
	mps.keys[mediaPlayerVolDownKeyID] = loadAssetImage("assets/volume-down-fill.png")
	mps.keys[mediaPlayerVolMuteKeyID] = loadAssetImage("assets/volume-mute-fill.png")
	mps.keys[mediaPlayerVolUpKeyID] = loadAssetImage("assets/volume-up-fill.png")

	homeScreen.RegisterScreen(mps)

	return mps
}

// SetPlaylistScreen configures the screen navigated to when the 'Playlist' button is pressed
func (mps *MediaPlayer) SetPlaylistScreen(screen deskpad.Screen) {
	mps.playlistScreen = screen
	mps.keys[mediaPlayerPlaylistKeyID] = screen.Icon()
}

// SetSettingsScreen configures the screen navigated to when the 'Settings' button is pressed
func (mps *MediaPlayer) SetSettingsScreen(screen deskpad.Screen) {
	mps.settingsScreen = screen
	mps.keys[mediaPlayerSettingsKeyID] = screen.Icon()
}

// Name is hardcoded to display as "media player"
func (mps *MediaPlayer) Name() string {
	return "media player"
}

// Icon returns the icon to display for this screen
func (mps *MediaPlayer) Icon() image.Image {
	return mps.iconImg
}

// Show returns the image set which will be shown to the user.
func (mps *MediaPlayer) Show() []image.Image {
	if mps.controller.IsPlaying() {
		mps.keys[mediaPlayerPlayPauseKeyID] = mps.pauseImg
	} else {
		mps.keys[mediaPlayerPlayPauseKeyID] = mps.playImg
	}

	if mps.controller.IsShuffle() {
		mps.keys[mediaPlayerShuffleLoopKeyID] = mps.loopImg
	} else {
		mps.keys[mediaPlayerShuffleLoopKeyID] = mps.shuffleImg
	}

	return mps.keys
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (mps *MediaPlayer) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == mediaPlayerPrevKeyID {
		mps.controller.Previous()
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerNextKeyID {
		mps.controller.Next()
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerShuffleLoopKeyID {
		if mps.controller.IsShuffle() {
			mps.controller.Shuffle(false)
			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.loopImg,
			}, nil
		} else {
			mps.controller.Shuffle(true)
			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.shuffleImg,
			}, nil
		}
	} else if id == mediaPlayerRewindKeyID {
		mps.controller.Rewind()
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerPlayPauseKeyID {
		if mps.controller.IsPlaying() {
			mps.controller.Pause()

			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.playImg,
			}, nil
		} else {
			mps.controller.Play()
			return deskpad.KeyPressAction{
				Action:  deskpad.KeyPressActionUpdateIcon,
				NewIcon: mps.pauseImg,
			}, nil
		}
	} else if id == mediaPlayerFastForwardKeyID {
		mps.controller.FastForward()
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerVolDownKeyID {
		mps.controller.VolumeDown()
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerVolMuteKeyID {
		if mps.controller.IsMuted() {
			mps.controller.Unmute()
		} else {
			mps.controller.Mute()
		}
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerVolUpKeyID {
		mps.controller.VolumeUp()
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionNoop,
		}, nil
	} else if id == mediaPlayerSettingsKeyID && mps.settingsScreen != nil {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.settingsScreen,
		}, nil
	} else if id == mediaPlayerHomeKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.homeScreen,
		}, nil
	} else if id == mediaPlayerPlaylistKeyID && mps.playlistScreen != nil {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mps.playlistScreen,
		}, nil
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, errors.New("unhandled key")
}
