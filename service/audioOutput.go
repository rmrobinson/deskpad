package service

import "image"

// AudioOutputType describes the different types of supported audio outputs
type AudioOutputType int

const (
	AudioOutputDeviceUnspecified = iota
	AudioOutputTypeComputer
	AudioOutputTypeSmartphone
	AudioOutputTypeSpeaker
)

// AudioOutput represents a controllable audio output device.
type AudioOutput struct {
	ID   string
	Name string

	Active bool
	Type   AudioOutputType
	Icon   image.Image
}
