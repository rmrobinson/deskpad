package service

import (
	"context"
	"fmt"
	"log"

	"github.com/lawl/pulseaudio"
	"github.com/rmrobinson/go-mpris"
)

// LinuxMediaPlayer uses the DBus MPRIS interface to control a media agent running on the local machine,
// and PulseAudio to control the output audio device settings.
// This supports the MediaPlayerController interface for audio control. It currently doesn't support listing
// media playlists - paired with a suitable MPRIS agent and the Spotify media player it can play the playlist
// URIs returned by the Spotify player playlist functions.
type LinuxMediaPlayer struct {
	mprisClient *mpris.Player

	paClient *pulseaudio.Client

	// TODO: add Bluetooth client
}

// NewLinuxMediaPlayer creates a new player using the supplied MRPIS and PulseAudio clients
func NewLinuxMediaPlayer(mprisClient *mpris.Player, paClient *pulseaudio.Client) *LinuxMediaPlayer {
	return &LinuxMediaPlayer{
		mprisClient: mprisClient,
		paClient:    paClient,
	}
}

func (m *LinuxMediaPlayer) Play(ctx context.Context) {
	m.mprisClient.Play()
}
func (m *LinuxMediaPlayer) Pause(ctx context.Context) {
	m.mprisClient.Pause()
}
func (m *LinuxMediaPlayer) Next(ctx context.Context) {
	m.mprisClient.Next()
}
func (m *LinuxMediaPlayer) Previous(ctx context.Context) {
	m.mprisClient.Previous()
}
func (m *LinuxMediaPlayer) FastForward(ctx context.Context) {
	pos := m.mprisClient.GetPosition()

	newPos := pos + 10000
	m.mprisClient.SeekTo(newPos)
}
func (m *LinuxMediaPlayer) Rewind(ctx context.Context) {
	pos := m.mprisClient.GetPosition()

	newPos := pos - 10000
	if newPos < 0 {
		newPos = 0
	}
	m.mprisClient.SeekTo(newPos)

}
func (m *LinuxMediaPlayer) VolumeUp(ctx context.Context) {
	v, err := m.paClient.Volume()
	if err != nil {
		log.Printf("error getting volume: %s\n", err.Error())
		return
	}

	v += 0.1
	if v > 1.0 {
		v = 1.0
	}
	m.paClient.SetVolume(v)
}

func (m *LinuxMediaPlayer) VolumeDown(ctx context.Context) {
	v, err := m.paClient.Volume()
	if err != nil {
		log.Printf("error getting volume: %s\n", err.Error())
		return
	}

	v -= 0.1
	if v < 0 {
		v = 0
	}
	m.paClient.SetVolume(v)
}

func (m *LinuxMediaPlayer) Mute(ctx context.Context) {
	m.paClient.SetMute(true)
}

func (m *LinuxMediaPlayer) Unmute(ctx context.Context) {
	m.paClient.SetMute(false)
}

func (m *LinuxMediaPlayer) Shuffle(ctx context.Context, shuffle bool) {
	m.mprisClient.SetShuffle(shuffle)
}

func (m *LinuxMediaPlayer) IsPlaying() bool {
	status := m.mprisClient.GetPlaybackStatus()

	return status == mpris.PlaybackPlaying
}
func (m *LinuxMediaPlayer) IsShuffle() bool {
	status := m.mprisClient.GetShuffle()
	return status
}
func (m *LinuxMediaPlayer) IsMuted() bool {
	muted, err := m.paClient.Mute()
	if err != nil {
		log.Printf("error getting muted state: %s\n", err.Error())
		return false
	}

	return muted
}

func (m *LinuxMediaPlayer) StartPlaylist(ctx context.Context, id string) {
	log.Printf("playing URI: %s\n", id)
	m.mprisClient.OpenUri(id)
}

func (m *LinuxMediaPlayer) CurrentlyPlaying(ctx context.Context) *MediaItem {
	if !m.IsPlaying() {
		return nil
	}

	metadata := m.mprisClient.GetMetadata()

	var artists []string
	for _, artist := range metadata["xesam:artist"].Value().([]string) {
		artists = append(artists, artist)
	}
	return &MediaItem{
		ID:           metadata["xesam:url"].Value().(string),
		Title:        metadata["xesam:title"].Value().(string),
		Artists:      artists,
		AlbumName:    metadata["xesam:album"].Value().(string),
		AlburmArtURL: metadata["mpris:artUrl"].Value().(string),
	}
}

func (m *LinuxMediaPlayer) GetAudioOutputs(ctx context.Context) []AudioOutput {
	sinks, err := m.paClient.Sinks()
	if err != nil {
		log.Printf("unable to get pulseaudio sinks: %s\n", err.Error())
		return []AudioOutput{}
	}

	var ret []AudioOutput
	for _, sink := range sinks {
		// State 0: active
		// State 2: suspended
		ret = append(ret, AudioOutput{
			ID:          fmt.Sprintf("%d", sink.Index),
			Name:        sink.Name,
			Description: sink.Description,
			Muted:       sink.Muted,
			Active:      sink.SinkState == 0,
		})
	}

	return ret
}
func (m *LinuxMediaPlayer) PlayOnDevice(_ context.Context, deviceID string) {
	// Set the default sink in PulseAudio
	m.paClient.SetDefaultSink(deviceID)
}
