package screens

import (
	"context"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui/controllers"
)

const (
	bluetoothSettingHomeKeyID    = 4
	bluetoothSettingRefreshKeyID = 9
	bluetoothSettingNextKeyID    = 14
)

// BluetoothSetting displays discoveredBluetooth devices and allows connecting to them.
type BluetoothSetting struct {
	iconImg    image.Image
	keys       []image.Image
	controller BluetoothSettingController

	homeScreen deskpad.Screen

	devices          []controllers.BluetoothDevice // keep a copy of the array to ensure a stable set when the button is pushed
	currDeviceOffset int
}

// BluetoothSettingController describes the functions which this screen will use to interact with the Bluetooth setting system.
type BluetoothSettingController interface {
	GetDevices() []controllers.BluetoothDevice
	RefreshDevices(context.Context) error
	ConnectDevice(controllers.BluetoothDevice) error
}

// MediaPlayerSetting creates a new instance of the media player setting screen, configured with the provided setting controller.
func NewBluetoothSetting(homeScreen *Home, bsc BluetoothSettingController) *BluetoothSetting {
	// Currently setup for a StreamDeck with 15 buttons
	bs := &BluetoothSetting{
		iconImg:    loadAssetImage("assets/bluetooth-fill.png"),
		keys:       make([]image.Image, 15),
		controller: bsc,
		homeScreen: homeScreen,
		devices:    []controllers.BluetoothDevice{},
	}

	bs.keys[bluetoothSettingHomeKeyID] = homeScreen.Icon()
	bs.keys[bluetoothSettingRefreshKeyID] = loadAssetImage("assets/refresh-fill.png")
	bs.keys[bluetoothSettingNextKeyID] = loadAssetImage("assets/skip-right-line.png")

	homeScreen.RegisterScreen(bs)

	return bs
}

// Name is hardcoded to display as "bluetooth setting"
func (bs *BluetoothSetting) Name() string {
	return "bluetooth setting"
}

// Icon returns the icon to display for this screen
func (bs *BluetoothSetting) Icon() image.Image {
	return bs.iconImg
}

// Show returns the image set which will be shown to the user.
func (bs *BluetoothSetting) Show() []image.Image {
	bs.devices = bs.controller.GetDevices()

	// Reset the icon set to avoid stale info being shown
	for i := 0; i < len(bs.keys); i++ {
		if i == bluetoothSettingHomeKeyID ||
			i == bluetoothSettingRefreshKeyID ||
			i == bluetoothSettingNextKeyID {
			continue
		}
		bs.keys[i] = nil
	}

	for devicePos, device := range bs.devices {
		var buttonImg image.Image
		if device.Connected() {
			deviceImg := loadAssetImage("assets/bluetooth-background-connect-fill.png")
			label := device.Name
			if len(label) < 1 {
				label = device.Address
			}
			buttonImg = NewTextIconWithBackground(label, deviceImg)
		} else {
			deviceImg := loadAssetImage("assets/bluetooth-background-fill.png")
			label := device.Name
			if len(label) < 1 {
				label = device.Address
			}
			buttonImg = NewTextIconWithBackground(label, deviceImg)
		}

		if devicePos <= 3 {
			bs.keys[devicePos] = buttonImg
		} else if devicePos > 3 && devicePos <= 7 {
			bs.keys[devicePos+1] = buttonImg
		} else if devicePos > 7 && devicePos <= 11 {
			bs.keys[devicePos+2] = buttonImg
		}
	}

	return bs.keys
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (bs *BluetoothSetting) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if t == deskpad.KeyPressLong {
		log.Print("got a long key press!\n")
	}

	if id == bluetoothSettingHomeKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: bs.homeScreen,
		}, nil
	} else if id == bluetoothSettingRefreshKeyID {
		bs.controller.RefreshDevices(ctx)
		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionRefreshScreen,
		}, nil
	} else if id == bluetoothSettingNextKeyID {
		bs.currDeviceOffset += 12
		// If we didn't get a full set, assume we're at the end and start over
		if len(bs.devices) < 12 {
			bs.currDeviceOffset = 0
		}

		return deskpad.KeyPressAction{
			Action: deskpad.KeyPressActionRefreshScreen,
		}, nil
	}

	deviceIdx := keyIDToDeviceIdx(id)
	if d := bs.devices[deviceIdx]; !d.Connected() {
		bs.controller.ConnectDevice(bs.devices[deviceIdx])
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}
