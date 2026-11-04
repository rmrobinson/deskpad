package ui

// DeviceKind describes the broad category of a controllable house device, used to
// pick which icon and which controls a device screen shows.
type DeviceKind int

const (
	DeviceKindOther DeviceKind = iota
	DeviceKindLight
	DeviceKindFan
	DeviceKindOutlet
	DeviceKindStandingDesk
)

// Device represents a controllable device linked to a configured room.
type Device struct {
	ID   string
	Name string
	Kind DeviceKind

	Online bool

	CanToggle bool
	On        bool

	CanDim        bool
	BrightnessPct int

	CanColour bool
	ColourR   int32
	ColourG   int32
	ColourB   int32
	// ColourUsesHSB is true when this device's colour trait is HSB-mode
	// rather than RGB-mode - the house protocol's RGB command value is only
	// valid for an RGB-mode device, so SetColour must convert to HSB before
	// sending a command to one of these, or the bridge silently ignores it.
	ColourUsesHSB bool

	CanAdjustSpeed bool
	Speed          int32
	MinSpeed       int32
	MaxSpeed       int32
	SpeedIncrement int32

	// StandMode/SitMode are the Mode trait's own preset values that raise or
	// lower a standing desk (e.g. "preset_3_stand"/"preset_4_sit") - empty
	// when the device has no such preset (or isn't a standing desk at all).
	StandMode string
	SitMode   string
}

// IsControllable reports whether this device has any trait DeskPad can
// actually command - devices with none (sensors, read-only state) are
// filtered out of the device list rather than shown with dead buttons.
func (d Device) IsControllable() bool {
	return d.CanToggle || d.CanDim || d.CanColour || d.CanAdjustSpeed ||
		d.StandMode != "" || d.SitMode != ""
}
