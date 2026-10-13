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

	return c.client.PlayOpt(ctx, playOptionsFor(deviceID, uri))
}

// playOptionsFor builds the play request for a URI on a device. Playlists, albums and shows are played as a
// context from their first item; single tracks and episodes can't be sent as a context and are played directly.
func playOptionsFor(deviceID spotify.ID, uri string) *spotify.PlayOptions {
	opts := &spotify.PlayOptions{DeviceID: &deviceID}

	if strings.HasPrefix(uri, "spotify:track:") || strings.HasPrefix(uri, "spotify:episode:") {
		opts.URIs = []spotify.URI{spotify.URI(uri)}
		return opts
	}

	contextURI := spotify.URI(uri)
	firstItem := 0
	opts.PlaybackContext = &contextURI
	opts.PlaybackOffset = &spotify.PlaybackOffset{Position: &firstItem}
	return opts
}

// findConnectDevice finds the device with the given name, ignoring case. Devices without an ID (Spotify reports
// restricted devices that way) can't be targeted, and if the name matches more than one the active one wins.
func findConnectDevice(devices []spotify.PlayerDevice, name string) (spotify.ID, bool) {
	var found spotify.ID
	for _, d := range devices {
		if d.ID == "" || !strings.EqualFold(d.Name, name) {
			continue
		}
		if d.Active {
			return d.ID, true
		}
		if found == "" {
			found = d.ID
		}
	}

	return found, found != ""
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
