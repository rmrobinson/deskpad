package service

import (
	"context"
	"image"
	"log"
	"net/http"

	"github.com/zmb3/spotify/v2"

	_ "golang.org/x/image/webp"
)

// SpotifyMediaPlayer uses the Spotify web API to control playback of a supported device.
// It supports the MediaPlayerController interface for playback control, along with the MediaPlaylistController
// to retrieve playlists and the MediaPlayerSettingController interface to switch between different, compatible Spotify-enabled devices.
// It caches some state information to ensure the UI is quick and responsive; if other tools are used to control the playback
// state of a device the RefreshPlayerState method should be used on an interval to keep the cached information in sync with reality.
type SpotifyMediaPlayer struct {
	client *spotify.Client

	prevVolume int
	isMuted    bool

	isPlaying bool
	isShuffle bool

	cachedPlaylists []MediaPlaylist
}

// NewSpotifyMediaPlayer creates a new media player using the supplied Spotify client.
func NewSpotifyMediaPlayer(client *spotify.Client) *SpotifyMediaPlayer {
	return &SpotifyMediaPlayer{
		client: client,
	}
}

// RefreshPlayerState is used to refresh the player and playlist cache information.
func (mp *SpotifyMediaPlayer) RefreshPlayerState(ctx context.Context) error {
	state, err := mp.client.PlayerState(context.Background())
	if err != nil {
		log.Printf("error getting is playing: %s\n", err.Error())
		return err
	}

	mp.isPlaying = state.CurrentlyPlaying.Playing
	mp.isShuffle = state.ShuffleState

	return nil
}

func (mp *SpotifyMediaPlayer) Play(ctx context.Context) {
	if err := mp.client.Play(ctx); err != nil {
		log.Printf("error playing: %s\n", err.Error())
	}
	mp.isPlaying = true
}

func (mp *SpotifyMediaPlayer) Pause(ctx context.Context) {
	if err := mp.client.Pause(ctx); err != nil {
		log.Printf("error pausing: %s\n", err.Error())
	}
	mp.isPlaying = false
}

func (mp *SpotifyMediaPlayer) Next(ctx context.Context) {
	if err := mp.client.Next(ctx); err != nil {
		log.Printf("error going next: %s\n", err.Error())
	}
}

func (mp *SpotifyMediaPlayer) Previous(ctx context.Context) {
	if err := mp.client.Previous(ctx); err != nil {
		log.Printf("error going previous: %s\n", err.Error())
	}
}

func (mp *SpotifyMediaPlayer) IsPlaying() bool {
	return mp.isPlaying
}

func (mp *SpotifyMediaPlayer) IsShuffle() bool {
	return mp.isShuffle
}

func (mp *SpotifyMediaPlayer) IsMuted() bool {
	return mp.isMuted
}

func (mp *SpotifyMediaPlayer) FastForward(ctx context.Context) {
	state, err := mp.client.PlayerState(ctx)
	if err != nil {
		log.Printf("error getting current position: %s\n", err.Error())
	}

	newTime := int(state.CurrentlyPlaying.Progress) + 10000

	if newTime > int(state.CurrentlyPlaying.Item.Duration) {
		log.Print("fast forwarding putting us to next song\n")
		mp.client.Next(ctx)
		return
	}

	if err := mp.client.Seek(ctx, newTime); err != nil {
		log.Printf("error fast forwarding 10 seconds: %s\n", err.Error())
	}
}

func (mp *SpotifyMediaPlayer) Rewind(ctx context.Context) {
	state, err := mp.client.PlayerState(ctx)
	if err != nil {
		log.Printf("error getting current position: %s\n", err.Error())
	}

	newTime := int(state.CurrentlyPlaying.Progress) - 10000

	if newTime < 0 {
		log.Print("fast forwarding putting us to start of song\n")
		newTime = 0
	}

	if err := mp.client.Seek(ctx, newTime); err != nil {
		log.Printf("error rewinding 10 seconds: %s\n", err.Error())
	}
}

func (mp *SpotifyMediaPlayer) Mute(ctx context.Context) {
	state, err := mp.client.PlayerState(ctx)
	if err != nil {
		log.Printf("error getting current volume: %s\n", err.Error())
		return
	}

	mp.prevVolume = int(state.Device.Volume)

	if err := mp.client.Volume(ctx, 0); err != nil {
		log.Printf("error muting device: %s\n", err.Error())
	}
	mp.isMuted = true
}

func (mp *SpotifyMediaPlayer) Unmute(ctx context.Context) {
	if err := mp.client.Volume(ctx, mp.prevVolume); err != nil {
		log.Printf("error unmuting device: %s\n", err.Error())
	}
	mp.isMuted = false
}

func (mp *SpotifyMediaPlayer) VolumeUp(ctx context.Context) {
	state, err := mp.client.PlayerState(ctx)
	if err != nil {
		log.Printf("error getting current volume: %s\n", err.Error())
		return
	}

	newVolume := int(state.Device.Volume) + 10
	if newVolume > 100 {
		newVolume = 100
	}

	if err := mp.client.Volume(ctx, newVolume); err != nil {
		log.Printf("error increasing volume: %s\n", err.Error())
	}
}

func (mp *SpotifyMediaPlayer) VolumeDown(ctx context.Context) {
	state, err := mp.client.PlayerState(ctx)
	if err != nil {
		log.Printf("error getting current volume: %s\n", err.Error())
		return
	}

	newVolume := int(state.Device.Volume) - 10
	if newVolume > 100 {
		newVolume = 100
	}

	if err := mp.client.Volume(ctx, newVolume); err != nil {
		log.Printf("error decreasing volume: %s\n", err.Error())
	}
}

func (mp *SpotifyMediaPlayer) Shuffle(ctx context.Context, shuffle bool) {
	err := mp.client.Shuffle(ctx, shuffle)
	if err != nil {
		log.Printf("error setting shuffle to %t: %s\n", shuffle, err.Error())
		return
	}
	mp.isShuffle = shuffle
}

func (mp *SpotifyMediaPlayer) StartPlaylist(ctx context.Context, id string) {
	playlistURI := spotify.URI(id)
	playlistOffset := 0
	opts := &spotify.PlayOptions{
		PlaybackContext: &playlistURI,
		PlaybackOffset:  &spotify.PlaybackOffset{Position: &playlistOffset},
	}

	if err := mp.client.PlayOpt(ctx, opts); err != nil {
		log.Printf("error playing new playlist: %s\n", err.Error())
	}

	log.Printf("playing URI: %s\n", id)
	mp.isPlaying = true
}

func (mp *SpotifyMediaPlayer) CurrentlyPlaying(ctx context.Context) *MediaItem {
	state, err := mp.client.PlayerState(ctx)
	if err != nil {
		log.Printf("error getting currently playing: %s\n", err.Error())
		return nil
	}

	if state.CurrentlyPlaying.Item != nil {
		var artists []string
		for _, artist := range state.CurrentlyPlaying.Item.Artists {
			artists = append(artists, artist.Name)
		}
		return &MediaItem{
			ID:        string(state.CurrentlyPlaying.Item.ID),
			Title:     state.CurrentlyPlaying.Item.Name,
			Artists:   artists,
			AlbumName: state.CurrentlyPlaying.Item.Album.Name,
		}
	}

	return nil
}

func (mp *SpotifyMediaPlayer) GetPlaylists(count int, offset int) []MediaPlaylist {
	startIdx := offset
	if startIdx > len(mp.cachedPlaylists) {
		startIdx = 0
	}

	endIdx := startIdx + count
	if endIdx > len(mp.cachedPlaylists) {
		endIdx = len(mp.cachedPlaylists)
	}

	return mp.cachedPlaylists[startIdx:endIdx]
}

func (mp *SpotifyMediaPlayer) RefreshPlaylists(ctx context.Context) error {
	mediaPlaylists := []MediaPlaylist{}

	playlists, err := mp.client.CurrentUsersPlaylists(ctx, spotify.Limit(50))
	if err != nil {
		log.Printf("error getting current playlists: %s\n", err.Error())
		return err
	}

	for _, playlist := range playlists.Playlists {
		log.Printf("got playlist %s with name %s\n", playlist.URI, playlist.Name)
		mediaPlaylist := MediaPlaylist{ID: string(playlist.URI), Name: playlist.Name}
		if len(playlist.Images) > 0 {
			imgURL := playlist.Images[0].URL
			resp, err := http.Get(imgURL)
			if err != nil {
				log.Printf("unable to download %s: %s\n", imgURL, err.Error())
				continue
			}
			defer resp.Body.Close()

			mediaPlaylist.Icon, _, err = image.Decode(resp.Body)
			if err != nil {
				log.Printf("unable to decode image for playlist %s at %s: %s\n", playlist.Name, imgURL, err.Error())
				continue
			}
		} else {
			log.Printf("playlist %s has no image\n", playlist.Name)
		}

		mediaPlaylists = append(mediaPlaylists, mediaPlaylist)
	}

	log.Printf("caching %d playlists\n", len(mediaPlaylists))
	mp.cachedPlaylists = mediaPlaylists
	return nil
}

func (mp *SpotifyMediaPlayer) GetAudioOutputs(ctx context.Context) []AudioOutput {
	mediaDevices := []AudioOutput{}

	devices, err := mp.client.PlayerDevices(ctx)
	if err != nil {
		log.Printf("error getting player devices: %s\n", err.Error())
		return mediaDevices
	}

	for _, device := range devices {
		if device.Restricted {
			continue
		}

		log.Printf("got device name %s with ID %s and type %s\n", device.Name, device.ID, device.Type)

		var deviceType AudioOutputType
		switch device.Type {
		case "Computer":
			deviceType = AudioOutputTypeComputer
		case "Smartphone":
			deviceType = AudioOutputTypeSmartphone
		case "Speaker":
			deviceType = AudioOutputTypeSpeaker
		}

		mediaDevices = append(mediaDevices, AudioOutput{
			ID:     string(device.ID),
			Name:   device.Name,
			Active: device.Active,
			Type:   deviceType,
		})
	}

	return mediaDevices
}

func (mp *SpotifyMediaPlayer) PlayOnDevice(ctx context.Context, deviceID string) {
	err := mp.client.TransferPlayback(ctx, spotify.ID(deviceID), true)
	if err != nil {
		log.Printf("error transfering playback to %s: %s\n", deviceID, err.Error())
	}
}
