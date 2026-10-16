package screens

import (
	"context"
	"image"
	"testing"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
)

type houseDeviceControlTestController struct {
	devices []ui.Device
	starts  int

	lastOnOffID string
	lastOn      bool
	onOffErr    error

	lastBrightnessID  string
	lastBrightnessPct int32

	lastSpeedID    string
	lastSpeedValue int32

	lastColourID string
	lastColourR  int32
	lastColourG  int32
	lastColourB  int32

	lastModeID    string
	lastModeValue string
}

func (c *houseDeviceControlTestController) Devices() []ui.Device {
	return c.devices
}

func (c *houseDeviceControlTestController) Start() {
	c.starts++
}

func (c *houseDeviceControlTestController) SetOnOff(ctx context.Context, deviceID string, on bool) error {
	c.lastOnOffID = deviceID
	c.lastOn = on
	return c.onOffErr
}

func (c *houseDeviceControlTestController) SetBrightness(ctx context.Context, deviceID string, pct int32) error {
	c.lastBrightnessID = deviceID
	c.lastBrightnessPct = pct
	return nil
}

func (c *houseDeviceControlTestController) SetColour(ctx context.Context, deviceID string, r, g, b int32) error {
	c.lastColourID = deviceID
	c.lastColourR, c.lastColourG, c.lastColourB = r, g, b
	return nil
}

func (c *houseDeviceControlTestController) SetSpeed(ctx context.Context, deviceID string, value int32) error {
	c.lastSpeedID = deviceID
	c.lastSpeedValue = value
	return nil
}

func (c *houseDeviceControlTestController) SetMode(ctx context.Context, deviceID string, mode string) error {
	c.lastModeID = deviceID
	c.lastModeValue = mode
	return nil
}

func newTestHouseDeviceControl(controller *houseDeviceControlTestController) *HouseDeviceControl {
	dc := &HouseDeviceControl{
		keys:       make([]image.Image, 15),
		controller: controller,
		listScreen: &Home{},
	}
	return dc
}

// A device removed from the room (e.g. unlinked, or dropped via a stream
// REMOVED update) must not keep showing stale, apparently-live controls, and
// key presses against it must not reach the controller.
func TestHouseDeviceControlShowsUnavailableAndIgnoresKeysWhenDeviceGone(t *testing.T) {
	controller := &houseDeviceControlTestController{devices: nil}
	dc := newTestHouseDeviceControl(controller)
	dc.SetDevice(ui.Device{ID: "light-1", Name: "Lamp", CanToggle: true, On: true})

	keys := dc.Show()

	if dc.available {
		t.Fatalf("available = true, want false once the device is gone from Devices()")
	}
	if keys[houseDeviceControlToggleKeyID] == nil {
		t.Fatalf("expected an \"unavailable\" indicator in the toggle slot, got nil")
	}
	if keys[houseDeviceControlDownKeyID] != nil || keys[houseDeviceControlUpKeyID] != nil {
		t.Fatalf("expected no dim/speed controls once the device is gone")
	}

	action, err := dc.KeyPressed(context.Background(), houseDeviceControlToggleKeyID, deskpad.KeyPressShort)
	if err != nil {
		t.Fatalf("KeyPressed returned error: %s", err)
	}
	if action.Action != deskpad.KeyPressActionNoop {
		t.Fatalf("action = %d, want Noop for a key press against an unavailable device", action.Action)
	}
	if controller.lastOnOffID != "" {
		t.Fatalf("SetOnOff was called (%q) against an unavailable device, want no command sent", controller.lastOnOffID)
	}

	// Back must still work even when the device is gone.
	action, err = dc.KeyPressed(context.Background(), houseDeviceControlBackKeyID, deskpad.KeyPressShort)
	if err != nil {
		t.Fatalf("KeyPressed(back) returned error: %s", err)
	}
	if action.Action != deskpad.KeyPressActionChangeScreen || action.NewScreen != dc.listScreen {
		t.Fatalf("back key did not return to the list screen")
	}
}

// Reappearing in Devices() (e.g. after a reconnect re-adds it) must clear
// the unavailable state and restore normal controls.
func TestHouseDeviceControlRecoversWhenDeviceReappears(t *testing.T) {
	controller := &houseDeviceControlTestController{devices: nil}
	dc := newTestHouseDeviceControl(controller)
	dc.SetDevice(ui.Device{ID: "light-1", Name: "Lamp", CanToggle: true})
	dc.Show()
	if dc.available {
		t.Fatalf("available = true before the device is present, want false")
	}

	controller.devices = []ui.Device{{ID: "light-1", Name: "Lamp", CanToggle: true, On: true}}
	keys := dc.Show()
	if !dc.available {
		t.Fatalf("available = false after the device reappeared, want true")
	}
	if keys[houseDeviceControlToggleKeyID] == nil {
		t.Fatalf("expected toggle control to be rendered again once the device is back")
	}
}

// A device that can both dim and adjust speed must expose both via the mode
// key, instead of silently only ever offering dim control.
func TestHouseDeviceControlModeKeyTogglesBetweenDimAndSpeed(t *testing.T) {
	controller := &houseDeviceControlTestController{}
	dc := newTestHouseDeviceControl(controller)
	dev := ui.Device{
		ID: "fan-1", Name: "Fan",
		CanDim: true, BrightnessPct: 50,
		CanAdjustSpeed: true, Speed: 2, MinSpeed: 0, MaxSpeed: 5, SpeedIncrement: 1,
	}
	controller.devices = []ui.Device{dev}
	dc.SetDevice(dev)

	keys := dc.Show()
	if keys[houseDeviceControlModeKeyID] == nil {
		t.Fatalf("expected a mode key when a device supports both dim and speed")
	}
	if dc.speedActive() {
		t.Fatalf("speedActive() = true by default, want dim to be the default target")
	}

	// Pressing up while in the default (dim) mode should adjust brightness.
	if _, err := dc.KeyPressed(context.Background(), houseDeviceControlUpKeyID, deskpad.KeyPressShort); err != nil {
		t.Fatalf("KeyPressed(up) returned error: %s", err)
	}
	if controller.lastBrightnessID != "fan-1" {
		t.Fatalf("expected a brightness command before switching modes, got lastBrightnessID=%q", controller.lastBrightnessID)
	}
	if controller.lastSpeedID != "" {
		t.Fatalf("expected no speed command before switching modes, got lastSpeedID=%q", controller.lastSpeedID)
	}

	// Flip to speed mode via the mode key.
	if _, err := dc.KeyPressed(context.Background(), houseDeviceControlModeKeyID, deskpad.KeyPressShort); err != nil {
		t.Fatalf("KeyPressed(mode) returned error: %s", err)
	}
	if !dc.speedActive() {
		t.Fatalf("speedActive() = false after pressing the mode key, want true")
	}

	controller.lastSpeedID = ""
	controller.lastBrightnessID = ""
	if _, err := dc.KeyPressed(context.Background(), houseDeviceControlUpKeyID, deskpad.KeyPressShort); err != nil {
		t.Fatalf("KeyPressed(up) returned error: %s", err)
	}
	if controller.lastSpeedID != "fan-1" {
		t.Fatalf("expected a speed command after switching modes, got lastSpeedID=%q", controller.lastSpeedID)
	}
	if controller.lastBrightnessID != "" {
		t.Fatalf("expected no brightness command after switching modes, got lastBrightnessID=%q", controller.lastBrightnessID)
	}
}
