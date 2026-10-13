package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/zmb3/spotify/v2"
)

// SpotifyConnect starts playback of a Spotify context (playlist, album, ...) on a named Spotify Connect device using
// the Spotify Web API. The call is made with deskpad's own Spotify app, which keeps it off the shared rate limit
// of third-party players such as ncspot, and Spotify itself tells the receiving device to start playing.
type SpotifyConnect struct {
	client *spotify.Client
}

// NewSpotifyConnect creates a Connect controller using the supplied Spotify client.
func NewSpotifyConnect(client *spotify.Client) *SpotifyConnect {
	return &SpotifyConnect{client: client}
}

// PlayContext starts the supplied Spotify URI on the Connect device with the given name, transferring playback to it
// if it isn't the active device.
func (c *SpotifyConnect) PlayContext(ctx context.Context, deviceName, uri string) error {
	devices, err := c.client.PlayerDevices(ctx)
	if err != nil {
		return fmt.Errorf("list spotify connect devices: %w", err)
	}

	deviceID, ok := findConnectDevice(devices, deviceName)
	if !ok {
		return fmt.Errorf("spotify connect device %q not found (available: %s)", deviceName, connectDeviceNames(devices))
	}

	contextURI := spotify.URI(uri)
	return c.client.PlayOpt(ctx, &spotify.PlayOptions{
		DeviceID:        &deviceID,
		PlaybackContext: &contextURI,
	})
}

func findConnectDevice(devices []spotify.PlayerDevice, name string) (spotify.ID, bool) {
	for _, d := range devices {
		if strings.EqualFold(d.Name, name) {
			return d.ID, true
		}
	}

	return "", false
}

func connectDeviceNames(devices []spotify.PlayerDevice) string {
	if len(devices) == 0 {
		return "none"
	}

	names := make([]string, 0, len(devices))
	for _, d := range devices {
		names = append(names, fmt.Sprintf("%q", d.Name))
	}
	return strings.Join(names, ", ")
}
