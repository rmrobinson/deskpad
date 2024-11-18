package ui

import (
	"context"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/service"
)

const (
	mediaPlayerSettingHomeKeyID    = 4
	mediaPlayerSettingPlayerKeyID  = 9
	mediaPlayerSettingRefreshKeyID = 14
)

// MediaPlayerSettingScreen displays configurable settings about the player to the user.
type MediaPlayerSettingScreen struct {
	mpsc service.MediaPlayerSettingController

	homeScreen   deskpad.Screen
	playerScreen deskpad.Screen

	devices []service.AudioOutput

	keys []image.Image
}

// MediaPlayerSettingScreen creates a new instance of the media player setting screen, configured with the provided setting controller.
func NewMediaPlayerSettingScreen(mpsc service.MediaPlayerSettingController) *MediaPlayerSettingScreen {
	// Currently setup for a StreamDeck with 15 buttons
	mpss := &MediaPlayerSettingScreen{
		mpsc:    mpsc,
		devices: []service.AudioOutput{},
		keys:    make([]image.Image, 15),
	}

	mpss.keys[mediaPlayerSettingHomeKeyID] = loadAssetImage("assets/home-3-fill.png")
	mpss.keys[mediaPlayerSettingPlayerKeyID] = loadAssetImage("assets/music-2-fill.png")
	mpss.keys[mediaPlayerSettingRefreshKeyID] = loadAssetImage("assets/refresh-fill.png")

	return mpss
}

// SetHomeScreen configures the screen navigated to when the 'Home' button is pressed
func (mpss *MediaPlayerSettingScreen) SetHomeScreen(screen deskpad.Screen) {
	mpss.homeScreen = screen
}

// SetPlayerScreen configures the screen navigated to when the 'Player' button is pressed
func (mpss *MediaPlayerSettingScreen) SetPlayerScreen(screen deskpad.Screen) {
	mpss.playerScreen = screen
}

// Name is hardcoded to display as "media player setting"
func (mpss *MediaPlayerSettingScreen) Name() string {
	return "media player setting"
}

// Show returns the image set which will be shown to the user.
func (mpss *MediaPlayerSettingScreen) Show() []image.Image {
	for devicePos, device := range mpss.devices {
		var deviceImg image.Image
		if device.Type == service.AudioOutputTypeComputer {
			computerImg := loadAssetImage("assets/computer-fill.png")
			deviceImg = NewTextIconWithBackground(device.Name, computerImg)
		} else if device.Type == service.AudioOutputTypeSmartphone {
			smartphoneImg := loadAssetImage("assets/smartphone-fill.png")
			deviceImg = NewTextIconWithBackground(device.Name, smartphoneImg)
		} else if device.Type == service.AudioOutputTypeSpeaker {
			speakerImg := loadAssetImage("assets/speaker-fill.png")
			deviceImg = NewTextIconWithBackground(device.Name, speakerImg)
		} else {
			deviceImg = NewTextIcon(device.Name)
		}

		if devicePos <= 3 {
			mpss.keys[devicePos] = deviceImg
		} else if devicePos > 3 && devicePos <= 7 {
			mpss.keys[devicePos+1] = deviceImg
		} else if devicePos > 7 && devicePos <= 11 {
			mpss.keys[devicePos+2] = deviceImg
		}
	}

	return mpss.keys
}

// RefreshDevices refreshes the set of devices this screen can display
func (mpss *MediaPlayerSettingScreen) RefreshDevices(ctx context.Context) {
	mpss.devices = mpss.mpsc.GetAudioOutputs(ctx)
	log.Printf("got %d devices\n", len(mpss.devices))
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (mpss *MediaPlayerSettingScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == mediaPlayerSettingHomeKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mpss.homeScreen,
		}, nil
	} else if id == mediaPlayerSettingPlayerKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: mpss.playerScreen,
		}, nil
	} else if id == mediaPlayerSettingRefreshKeyID {
		mpss.RefreshDevices(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionRefreshScreen,
		}, nil
	}

	deviceIdx := keyIDToDeviceIdx(id)
	mpss.mpsc.PlayOnDevice(ctx, mpss.devices[deviceIdx].ID)

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}

func keyIDToDeviceIdx(id int) int {
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
