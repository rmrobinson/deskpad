package screens

import (
	"context"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
)

const (
	houseDeviceListHomeKeyID    = 4
	houseDeviceListRefreshKeyID = 9
	houseDeviceListNextKeyID    = 14
)

// HouseDeviceListController describes the functions this screen uses to
// interact with a configured room's devices.
type HouseDeviceListController interface {
	Devices() []ui.Device
	RefreshRoom(ctx context.Context) error
	Start()
	Stop()
}

// HouseDeviceSelector is implemented by the device control screen a
// HouseDeviceList navigates to when a device button is pressed.
type HouseDeviceSelector interface {
	deskpad.Screen
	SetDevice(d ui.Device)
}

// HouseDeviceList displays the controllable devices linked to one configured
// room, paginated 12 at a time. Selecting a device navigates to the paired
// control screen; the controller's update stream is only kept open while
// this screen (or the paired control screen) is on screen.
type HouseDeviceList struct {
	name    string
	iconImg image.Image
	keys    []image.Image

	controller    HouseDeviceListController
	homeScreen    deskpad.Screen
	controlScreen HouseDeviceSelector

	devices          []ui.Device // keep a copy to ensure a stable set when a button is pushed
	currDeviceOffset int
}

// NewHouseDeviceList creates a device-list screen for one room, labelled
// name, and registers it on homeScreen.
func NewHouseDeviceList(homeScreen *Home, name string, c HouseDeviceListController) *HouseDeviceList {
	dl := &HouseDeviceList{
		name:       name,
		iconImg:    NewRoomIcon(),
		keys:       make([]image.Image, 15),
		controller: c,
		homeScreen: homeScreen,
		devices:    []ui.Device{},
	}

	dl.keys[houseDeviceListHomeKeyID] = homeScreen.Icon()
	dl.keys[houseDeviceListRefreshKeyID] = loadAssetImage("assets/refresh-fill.png")
	dl.keys[houseDeviceListNextKeyID] = loadAssetImage("assets/skip-right-line.png")

	homeScreen.RegisterScreen(dl)

	return dl
}

// SetControlScreen configures the screen navigated to when a device button is pressed.
func (dl *HouseDeviceList) SetControlScreen(screen HouseDeviceSelector) {
	dl.controlScreen = screen
}

// Name returns the room label this screen was configured with.
func (dl *HouseDeviceList) Name() string {
	return dl.name
}

// Icon returns the icon to display for this screen.
func (dl *HouseDeviceList) Icon() image.Image {
	return dl.iconImg
}

// Show returns the image set which will be shown to the user.
func (dl *HouseDeviceList) Show() []image.Image {
	dl.controller.Start()
	dl.devices = dl.controller.Devices()

	for i := 0; i < len(dl.keys); i++ {
		if i == houseDeviceListHomeKeyID || i == houseDeviceListRefreshKeyID || i == houseDeviceListNextKeyID {
			continue
		}
		dl.keys[i] = nil
	}

	page := dl.currentPage()
	for pagePos, d := range page {
		dl.keys[playlistIdxToKeyID(pagePos)] = NewTextIconWithBackground(d.Name, NewKindBadge(d.Kind))
	}

	return dl.keys
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (dl *HouseDeviceList) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if id == houseDeviceListHomeKeyID {
		dl.controller.Stop()
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: dl.homeScreen,
		}, nil
	} else if id == houseDeviceListRefreshKeyID {
		if err := dl.controller.RefreshRoom(ctx); err != nil {
			log.Printf("house device list: error refreshing room: %s\n", err.Error())
		}
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionRefreshScreen,
		}, nil
	} else if id == houseDeviceListNextKeyID {
		dl.currDeviceOffset += 12
		if dl.currDeviceOffset >= len(dl.devices) {
			dl.currDeviceOffset = 0
		}
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionRefreshScreen,
		}, nil
	}

	if dl.controlScreen == nil {
		return deskpad.KeyPressAction{Action: deskpad.KeyPressActionNoop}, nil
	}

	pagePos := keyIDToDeviceIdx(id)
	page := dl.currentPage()
	if pagePos < 0 || pagePos >= len(page) {
		return deskpad.KeyPressAction{Action: deskpad.KeyPressActionNoop}, nil
	}

	dl.controlScreen.SetDevice(page[pagePos])

	return deskpad.KeyPressAction{
		Action:    deskpad.KeyPressActionChangeScreen,
		NewScreen: dl.controlScreen,
	}, nil
}

func (dl *HouseDeviceList) currentPage() []ui.Device {
	if dl.currDeviceOffset >= len(dl.devices) {
		return nil
	}
	end := dl.currDeviceOffset + 12
	if end > len(dl.devices) {
		end = len(dl.devices)
	}
	return dl.devices[dl.currDeviceOffset:end]
}
