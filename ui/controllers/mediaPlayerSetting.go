package controllers

import (
	"context"
	"fmt"
	"log"

	"github.com/lawl/pulseaudio"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/zmb3/spotify/v2"
)

// MediaPlayerSetting is a controller which facilitates media playback on the available audio output devices.
type MediaPlayerSetting struct {
	spotifyClient *spotify.Client
	paClient      *pulseaudio.Client

	cachedAudioOutputs []ui.AudioOutput
}

// NewMediaPlayerSetting creates a new media player setting controller. If the pulseAudio client isn't supplied,
// it will currently default to use spotify.
func NewMediaPlayerSetting(sc *spotify.Client, pac *pulseaudio.Client) *MediaPlayerSetting {
	return &MediaPlayerSetting{
		spotifyClient: sc,
		paClient:      pac,
	}
}

// GetAudioOutputs returns the list of available audio outputs.
func (mps *MediaPlayerSetting) GetAudioOutputs() []ui.AudioOutput {
	return mps.cachedAudioOutputs
}

func (mps *MediaPlayerSetting) RefreshAudioOutputs(ctx context.Context) error {
	var audioOutputs []ui.AudioOutput

	if mps.paClient != nil {
		sinks, err := mps.paClient.Sinks()
		if err != nil {
			log.Printf("unable to get pulseaudio sinks: %s\n", err.Error())
			return err
		}

		for _, sink := range sinks {
			// State 0: active
			// State 2: suspended
			audioOutputs = append(audioOutputs, ui.AudioOutput{
				ID:          fmt.Sprintf("%d", sink.Index),
				Name:        sink.Description,
				Description: sink.Name,
				Muted:       sink.Muted,
				Active:      sink.SinkState == 0,
			})
		}
	} else {
		devices, err := mps.spotifyClient.PlayerDevices(ctx)
		if err != nil {
			log.Printf("error getting player devices: %s\n", err.Error())
			return err
		}

		for _, device := range devices {
			// If we can't control playback on the device, don't display it
			if device.Restricted {
				continue
			}

			var deviceType ui.AudioOutputType
			switch device.Type {
			case "Computer":
				deviceType = ui.AudioOutputTypeComputer
			case "Smartphone":
				deviceType = ui.AudioOutputTypeSmartphone
			case "Speaker":
				deviceType = ui.AudioOutputTypeSpeaker
			}

			audioOutputs = append(audioOutputs, ui.AudioOutput{
				ID:     string(device.ID),
				Name:   device.Name,
				Active: device.Active,
				Type:   deviceType,
			})
		}
	}

	log.Printf("caching %d audio outputs\n", len(audioOutputs))
	mps.cachedAudioOutputs = audioOutputs
	return nil
}

// SelectAudioOutput directs the active media playback stream to the specified device ID.
func (mps *MediaPlayerSetting) SelectAudioOutput(ctx context.Context, deviceID string) {
	if mps.paClient != nil {
		// Set the default sink in PulseAudio
		mps.paClient.SetDefaultSink(deviceID)
	} else {
		// Transfer playback to the supplied device ID
		err := mps.spotifyClient.TransferPlayback(ctx, spotify.ID(deviceID), true)
		if err != nil {
			log.Printf("error transfering playback to %s: %s\n", deviceID, err.Error())
		}
	}
}
