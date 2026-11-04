package screens

import (
	"context"
	"image"
	"log"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
)

const (
	houseDeviceControlToggleKeyID = 0
	houseDeviceControlDownKeyID   = 1
	houseDeviceControlUpKeyID     = 2
	houseDeviceControlNameKeyID   = 3
	houseDeviceControlBackKeyID   = 4
	// houseDeviceControlModeKeyID switches what the shared up/down keys
	// adjust, for the rare device that supports both dimming and speed.
	houseDeviceControlModeKeyID = 13
)

// houseDeviceControlColourKeyIDs are the slots used to show colour-preset
// swatches, in order.
var houseDeviceControlColourKeyIDs = []int{5, 6, 7, 8, 9, 10, 11, 12}

// houseColourPresets is the fixed palette of colours shown on a colour-
// capable device's control screen - a continuous colour wheel doesn't fit a
// discrete button grid, so a small fixed set of presets is offered instead.
var houseColourPresets = []struct{ R, G, B int32 }{
	{255, 0, 0},
	{255, 140, 0},
	{255, 230, 0},
	{0, 200, 0},
	{0, 90, 255},
	{160, 0, 220},
	{255, 255, 255},
	{255, 200, 120},
}

const brightnessStepPct = 10

// HouseDeviceControlController describes the functions this screen uses to
// read and command a room's devices.
type HouseDeviceControlController interface {
	Devices() []ui.Device
	SetOnOff(ctx context.Context, deviceID string, on bool) error
	SetBrightness(ctx context.Context, deviceID string, pct int32) error
	SetColour(ctx context.Context, deviceID string, r, g, b int32) error
	SetSpeed(ctx context.Context, deviceID string, value int32) error
	SetMode(ctx context.Context, deviceID string, mode string) error
	Start()
}

// HouseDeviceControl is a shared detail screen showing on/off, dim, colour,
// and speed controls as applicable to whichever device SetDevice last
// configured it with.
type HouseDeviceControl struct {
	iconImg image.Image
	keys    []image.Image

	controller HouseDeviceControlController
	listScreen deskpad.Screen

	device    ui.Device
	available bool // false once the device disappears from the controller (removed/unlinked)

	// adjustSpeed selects which trait the shared up/down keys target when a
	// device supports both dimming and speed; only meaningful in that case.
	adjustSpeed bool
}

// NewHouseDeviceControl creates a device control screen paired with listScreen.
func NewHouseDeviceControl(listScreen deskpad.Screen, c HouseDeviceControlController) *HouseDeviceControl {
	dc := &HouseDeviceControl{
		iconImg:    NewRoomIcon(),
		keys:       make([]image.Image, 15),
		controller: c,
		listScreen: listScreen,
	}

	dc.keys[houseDeviceControlBackKeyID] = loadAssetImage("assets/skip-back-fill.png")

	return dc
}

// SetDevice configures which device this screen controls. Called by the
// device-list screen just before navigating here.
func (dc *HouseDeviceControl) SetDevice(d ui.Device) {
	dc.device = d
	dc.available = true
	dc.adjustSpeed = false
}

// Name is hardcoded to display as "house device control"
func (dc *HouseDeviceControl) Name() string {
	return "house device control"
}

// Icon returns the icon to display for this screen.
func (dc *HouseDeviceControl) Icon() image.Image {
	return dc.iconImg
}

// Show returns the image set which will be shown to the user.
func (dc *HouseDeviceControl) Show() []image.Image {
	dc.controller.Start()

	// Pick up anything the update stream (or our own last command) has
	// changed since SetDevice was called.
	dc.available = false
	for _, d := range dc.controller.Devices() {
		if d.ID == dc.device.ID {
			dc.device = d
			dc.available = true
			break
		}
	}

	for i := range dc.keys {
		if i == houseDeviceControlBackKeyID {
			continue
		}
		dc.keys[i] = nil
	}

	dc.keys[houseDeviceControlNameKeyID] = NewTextIcon(dc.device.Name)

	if !dc.available {
		// The device was unlinked/removed since we navigated here (or since
		// the last refresh); showing stale on/off/dim/colour controls for it
		// would look live but every command against it would just fail
		// bridge-side, so show nothing but the fact it's gone instead.
		dc.keys[houseDeviceControlToggleKeyID] = NewTextIcon("unavailable")
		return dc.keys
	}

	if dc.device.CanToggle {
		dc.keys[houseDeviceControlToggleKeyID] = NewOnOffBadge(dc.device.On)
	}
	if dc.device.CanDim || dc.device.CanAdjustSpeed {
		if dc.speedActive() {
			dc.keys[houseDeviceControlDownKeyID] = NewStepIcon("assets/slow-down-line.png")
			dc.keys[houseDeviceControlUpKeyID] = NewStepIcon("assets/speed-up-line.png")
		} else {
			dc.keys[houseDeviceControlDownKeyID] = NewStepIcon("assets/arrow-down-s-line.png")
			dc.keys[houseDeviceControlUpKeyID] = NewStepIcon("assets/arrow-up-s-line.png")
		}
	}
	if dc.device.CanDim && dc.device.CanAdjustSpeed {
		// Both traits are present (e.g. a Generic device exposing both) but
		// the up/down keys can only target one at a time - offer a key to
		// flip which one, rather than silently hiding whichever lost out.
		dc.keys[houseDeviceControlModeKeyID] = NewTextIcon("DIM/SPD")
	}
	if !dc.device.CanDim && !dc.device.CanAdjustSpeed {
		// Standing desk presets share the same up/down keys dim/speed would
		// otherwise use - mutually exclusive by device kind, so no conflict.
		if dc.device.SitMode != "" {
			dc.keys[houseDeviceControlDownKeyID] = loadAssetImage("assets/corner-right-down-line.png")
		}
		if dc.device.StandMode != "" {
			dc.keys[houseDeviceControlUpKeyID] = loadAssetImage("assets/corner-left-up-line.png")
		}
	}
	if dc.device.CanColour {
		for i, preset := range houseColourPresets {
			if i >= len(houseDeviceControlColourKeyIDs) {
				break
			}
			selected := dc.device.ColourR == preset.R && dc.device.ColourG == preset.G && dc.device.ColourB == preset.B
			dc.keys[houseDeviceControlColourKeyIDs[i]] = NewColourSwatch(preset.R, preset.G, preset.B, selected)
		}
	}
	if !dc.device.CanToggle && !dc.device.CanDim && !dc.device.CanColour && !dc.device.CanAdjustSpeed &&
		dc.device.StandMode == "" && dc.device.SitMode == "" {
		dc.keys[houseDeviceControlToggleKeyID] = NewTextIcon("no controls")
	}

	return dc.keys
}

// speedActive reports whether the shared up/down keys currently target speed
// rather than brightness. When a device only has one of the two traits,
// that trait is always the target; when it has both, dc.adjustSpeed (toggled
// by the mode key) decides.
func (dc *HouseDeviceControl) speedActive() bool {
	if dc.device.CanDim && dc.device.CanAdjustSpeed {
		return dc.adjustSpeed
	}
	return dc.device.CanAdjustSpeed
}

// KeyPressed handles the logic of what to do when a given key is pressed.
func (dc *HouseDeviceControl) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if id == houseDeviceControlBackKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: dc.listScreen,
		}, nil
	}

	if !dc.available {
		return deskpad.KeyPressAction{Action: deskpad.KeyPressActionNoop}, nil
	}

	switch {
	case id == houseDeviceControlToggleKeyID && dc.device.CanToggle:
		on := !dc.device.On
		if err := dc.controller.SetOnOff(ctx, dc.device.ID, on); err != nil {
			log.Printf("house device control: error setting on/off: %s\n", err.Error())
			break
		}
		dc.device.On = on
	case id == houseDeviceControlModeKeyID && dc.device.CanDim && dc.device.CanAdjustSpeed:
		dc.adjustSpeed = !dc.adjustSpeed
	case id == houseDeviceControlDownKeyID && dc.speedActive():
		dc.applySpeed(ctx, dc.device.Speed-dc.speedStep())
	case id == houseDeviceControlUpKeyID && dc.speedActive():
		dc.applySpeed(ctx, dc.device.Speed+dc.speedStep())
	case id == houseDeviceControlDownKeyID && dc.device.CanDim:
		dc.applyBrightness(ctx, dc.device.BrightnessPct-brightnessStepPct)
	case id == houseDeviceControlUpKeyID && dc.device.CanDim:
		dc.applyBrightness(ctx, dc.device.BrightnessPct+brightnessStepPct)
	case id == houseDeviceControlDownKeyID && dc.device.SitMode != "":
		if err := dc.controller.SetMode(ctx, dc.device.ID, dc.device.SitMode); err != nil {
			log.Printf("house device control: error setting mode: %s\n", err.Error())
			break
		}
	case id == houseDeviceControlUpKeyID && dc.device.StandMode != "":
		if err := dc.controller.SetMode(ctx, dc.device.ID, dc.device.StandMode); err != nil {
			log.Printf("house device control: error setting mode: %s\n", err.Error())
			break
		}
	case dc.device.CanColour && colourKeyIndex(id) >= 0 && colourKeyIndex(id) < len(houseColourPresets):
		preset := houseColourPresets[colourKeyIndex(id)]
		if err := dc.controller.SetColour(ctx, dc.device.ID, preset.R, preset.G, preset.B); err != nil {
			log.Printf("house device control: error setting colour: %s\n", err.Error())
			break
		}
		dc.device.ColourR, dc.device.ColourG, dc.device.ColourB = preset.R, preset.G, preset.B
	default:
		return deskpad.KeyPressAction{Action: deskpad.KeyPressActionNoop}, nil
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionRefreshScreen,
	}, nil
}

func (dc *HouseDeviceControl) applyBrightness(ctx context.Context, pct int) {
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	if err := dc.controller.SetBrightness(ctx, dc.device.ID, int32(pct)); err != nil {
		log.Printf("house device control: error setting brightness: %s\n", err.Error())
		return
	}
	dc.device.BrightnessPct = pct
}

func (dc *HouseDeviceControl) speedStep() int32 {
	if dc.device.SpeedIncrement > 0 {
		return dc.device.SpeedIncrement
	}
	return 1
}

func (dc *HouseDeviceControl) applySpeed(ctx context.Context, value int32) {
	if value < dc.device.MinSpeed {
		value = dc.device.MinSpeed
	} else if value > dc.device.MaxSpeed {
		value = dc.device.MaxSpeed
	}
	if err := dc.controller.SetSpeed(ctx, dc.device.ID, value); err != nil {
		log.Printf("house device control: error setting speed: %s\n", err.Error())
		return
	}
	dc.device.Speed = value
}

func colourKeyIndex(id int) int {
	for i, keyID := range houseDeviceControlColourKeyIDs {
		if keyID == id {
			return i
		}
	}
	return -1
}
