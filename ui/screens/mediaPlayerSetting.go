package screens

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

// MediaPlayerSetting displays configurable settings about the player to the user.
type MediaPlayerSetting struct {
	iconImg    image.Image
	keys       []image.Image
	controller MediaPlayerSettingController

	homeScreen   deskpad.Screen
	playerScreen deskpad.Screen

	audioOutputs []service.AudioOutput
}

// MediaPlayerSettingController describes the functions which this screen will use to interact with the player setting source.
type MediaPlayerSettingController interface {
	GetAudioOutputs() []service.AudioOutput
	RefreshAudioOutputs(context.Context) error
	SelectAudioOutput(ctx context.Context, deviceID string)
}

// MediaPlayerSetting creates a new instance of the media player setting screen, configured with the provided setting controller.
func NewMediaPlayerSetting(homeScreen *Home, mpsc MediaPlayerSettingController) *MediaPlayerSetting {
	// Currently setup for a StreamDeck with 15 buttons
	mpss := &MediaPlayerSetting{
		iconImg:      loadAssetImage("assets/settings-3-fill.png"),
		keys:         make([]image.Image, 15),
		controller:   mpsc,
		homeScreen:   homeScreen,
		audioOutputs: []service.AudioOutput{},
	}

	mpss.keys[mediaPlayerSettingHomeKeyID] = homeScreen.Icon()
	mpss.keys[mediaPlayerSettingRefreshKeyID] = loadAssetImage("assets/refresh-fill.png")

	return mpss
}

// SetPlayerScreen configures the screen navigated to when the 'Player' button is pressed
func (mpss *MediaPlayerSetting) SetPlayerScreen(screen deskpad.Screen) {
	mpss.playerScreen = screen
	mpss.keys[mediaPlayerSettingPlayerKeyID] = screen.Icon()
}

// Name is hardcoded to display as "media player setting"
func (mpss *MediaPlayerSetting) Name() string {
	return "media player setting"
}

// Icon returns the icon to display for this screen
func (mpss *MediaPlayerSetting) Icon() image.Image {
	return mpss.iconImg
}

// RefreshDevices refreshes the set of devices this screen can display
func (mpss *MediaPlayerSetting) RefreshAudioOutputs() {
	mpss.audioOutputs = mpss.controller.GetAudioOutputs()
	log.Printf("got %d devices\n", len(mpss.audioOutputs))
}

// Show returns the image set which will be shown to the user.
func (mpss *MediaPlayerSetting) Show() []image.Image {
	for devicePos, device := range mpss.audioOutputs {
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

// KeyPressed handles the logic of what to do when a given key is pressed.
func (mpss *MediaPlayerSetting) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
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
		mpss.RefreshAudioOutputs()
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionRefreshScreen,
		}, nil
	}

	deviceIdx := keyIDToDeviceIdx(id)
	mpss.controller.SelectAudioOutput(ctx, mpss.audioOutputs[deviceIdx].ID)

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