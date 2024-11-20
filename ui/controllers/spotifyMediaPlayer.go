package controllers

import (
	"context"
	"log"

	"github.com/rmrobinson/deskpad/ui"
	"github.com/zmb3/spotify/v2"
)

// SpotifyMediaPlayer uses the Spotify web API to control media playback of a supported device.
// It supports the MediaPlayerController interface for playback control.
type SpotifyMediaPlayer struct {
	client *spotify.Client
	ctx    context.Context

	prevVolume int
	isMuted    bool

	isPlaying bool
	isShuffle bool
}

// NewSpotifyMediaPlayer creates a new media player using the supplied Spotify client.
func NewSpotifyMediaPlayer(ctx context.Context, client *spotify.Client) *SpotifyMediaPlayer {
	return &SpotifyMediaPlayer{
		ctx:    ctx,
		client: client,
	}
}

// RefreshPlayerState is used to refresh the player and playlist cache information.
func (mp *SpotifyMediaPlayer) RefreshPlayerState() error {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("error getting is playing: %w\n", err)
		return err
	}

	mp.isPlaying = state.CurrentlyPlaying.Playing
	mp.isShuffle = state.ShuffleState

	return nil
}

func (mp *SpotifyMediaPlayer) ID() string {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("unable to get player state: %w\n", err)
		return ""
	}

	return state.Device.ID.String()
}

func (mp *SpotifyMediaPlayer) Play() {
	if err := mp.client.Play(mp.ctx); err != nil {
		log.Printf("error playing: %w\n", err)
	}
	mp.isPlaying = true
}

func (mp *SpotifyMediaPlayer) Pause() {
	if err := mp.client.Pause(mp.ctx); err != nil {
		log.Printf("error pausing: %w\n", err)
	}
	mp.isPlaying = false
}

func (mp *SpotifyMediaPlayer) Next() {
	if err := mp.client.Next(mp.ctx); err != nil {
		log.Printf("error going next: %w\n", err)
	}
}

func (mp *SpotifyMediaPlayer) Previous() {
	if err := mp.client.Previous(mp.ctx); err != nil {
		log.Printf("error going previous: %w\n", err)
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

func (mp *SpotifyMediaPlayer) FastForward() {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("error getting current position: %w\n", err)
	}

	newTime := int(state.CurrentlyPlaying.Progress) + 10000

	if newTime > int(state.CurrentlyPlaying.Item.Duration) {
		log.Print("fast forwarding putting us to next song\n")
		mp.client.Next(mp.ctx)
		return
	}

	if err := mp.client.Seek(mp.ctx, newTime); err != nil {
		log.Printf("error fast forwarding 10 seconds: %w\n", err)
	}
}

func (mp *SpotifyMediaPlayer) Rewind() {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("error getting current position: %w\n", err)
	}

	newTime := int(state.CurrentlyPlaying.Progress) - 10000

	if newTime < 0 {
		log.Print("fast forwarding putting us to start of song\n")
		newTime = 0
	}

	if err := mp.client.Seek(mp.ctx, newTime); err != nil {
		log.Printf("error rewinding 10 seconds: %w\n", err)
	}
}

func (mp *SpotifyMediaPlayer) Mute() {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("error getting current volume: %w\n", err)
		return
	}

	mp.prevVolume = int(state.Device.Volume)

	if err := mp.client.Volume(mp.ctx, 0); err != nil {
		log.Printf("error muting device: %w\n", err)
	}
	mp.isMuted = true
}

func (mp *SpotifyMediaPlayer) Unmute() {
	if err := mp.client.Volume(mp.ctx, mp.prevVolume); err != nil {
		log.Printf("error unmuting device: %w\n", err)
	}
	mp.isMuted = false
}

func (mp *SpotifyMediaPlayer) VolumeUp() {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("error getting current volume: %w\n", err)
		return
	}

	newVolume := int(state.Device.Volume) + 10
	if newVolume > 100 {
		newVolume = 100
	}

	if err := mp.client.Volume(mp.ctx, newVolume); err != nil {
		log.Printf("error increasing volume: %w\n", err)
	}
}

func (mp *SpotifyMediaPlayer) VolumeDown() {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("error getting current volume: %w\n", err)
		return
	}

	newVolume := int(state.Device.Volume) - 10
	if newVolume > 100 {
		newVolume = 100
	}

	if err := mp.client.Volume(mp.ctx, newVolume); err != nil {
		log.Printf("error decreasing volume: %w\n", err)
	}
}

func (mp *SpotifyMediaPlayer) Shuffle(shuffle bool) {
	err := mp.client.Shuffle(mp.ctx, shuffle)
	if err != nil {
		log.Printf("error setting shuffle to %t: %s\n", shuffle, err.Error())
		return
	}
	mp.isShuffle = shuffle
}

func (mp *SpotifyMediaPlayer) CurrentlyPlaying() *ui.MediaItem {
	state, err := mp.client.PlayerState(mp.ctx)
	if err != nil {
		log.Printf("error getting currently playing: %w\n", err)
		return nil
	}

	if state.CurrentlyPlaying.Item != nil {
		var artists []string
		for _, artist := range state.CurrentlyPlaying.Item.Artists {
			artists = append(artists, artist.Name)
		}
		return &ui.MediaItem{
			ID:        string(state.CurrentlyPlaying.Item.ID),
			Title:     state.CurrentlyPlaying.Item.Name,
			Artists:   artists,
			AlbumName: state.CurrentlyPlaying.Item.Album.Name,
		}
	}

	return nil
}
