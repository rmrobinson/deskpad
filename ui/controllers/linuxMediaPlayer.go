package controllers

import (
	"log"

	"github.com/lawl/pulseaudio"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/go-mpris"
)

// LinuxMediaPlayer uses the DBus MPRIS interface to control a media agent running on the local machine,
// and PulseAudio to control the output audio device settings.
// This supports the MediaPlayerController interface for audio control. It currently doesn't support listing
// media playlists - paired with a suitable MPRIS agent and the Spotify media player it can play the playlist
// URIs returned by the Spotify player playlist functions.
type LinuxMediaPlayer struct {
	mprisClient       *mpris.Player
	mprisInstanceName string

	paClient *pulseaudio.Client

	// TODO: add Bluetooth client
}

// NewLinuxMediaPlayer creates a new player using the supplied MRPIS and PulseAudio clients
func NewLinuxMediaPlayer(mprisClient *mpris.Player, mprisInstanceName string, paClient *pulseaudio.Client) *LinuxMediaPlayer {
	return &LinuxMediaPlayer{
		mprisClient:       mprisClient,
		mprisInstanceName: mprisInstanceName,
		paClient:          paClient,
	}
}

func (m *LinuxMediaPlayer) ID() string {
	return m.mprisInstanceName
}

func (m *LinuxMediaPlayer) Play() {
	m.mprisClient.Play()
}
func (m *LinuxMediaPlayer) Pause() {
	m.mprisClient.Pause()
}
func (m *LinuxMediaPlayer) Next() {
	m.mprisClient.Next()
}
func (m *LinuxMediaPlayer) Previous() {
	m.mprisClient.Previous()
}
func (m *LinuxMediaPlayer) FastForward() {
	pos := m.mprisClient.GetPosition()

	newPos := pos + 10000
	m.mprisClient.SeekTo(newPos)
}
func (m *LinuxMediaPlayer) Rewind() {
	pos := m.mprisClient.GetPosition()

	newPos := pos - 10000
	if newPos < 0 {
		newPos = 0
	}
	m.mprisClient.SeekTo(newPos)

}
func (m *LinuxMediaPlayer) VolumeUp() {
	v, err := m.paClient.Volume()
	if err != nil {
		log.Printf("error getting volume: %w\n", err)
		return
	}

	v += 0.1
	if v > 1.0 {
		v = 1.0
	}
	m.paClient.SetVolume(v)
}

func (m *LinuxMediaPlayer) VolumeDown() {
	v, err := m.paClient.Volume()
	if err != nil {
		log.Printf("error getting volume: %w\n", err)
		return
	}

	v -= 0.1
	if v < 0 {
		v = 0
	}
	m.paClient.SetVolume(v)
}

func (m *LinuxMediaPlayer) Mute() {
	m.paClient.SetMute(true)
}

func (m *LinuxMediaPlayer) Unmute() {
	m.paClient.SetMute(false)
}

func (m *LinuxMediaPlayer) Shuffle(shuffle bool) {
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
		log.Printf("error getting muted state: %w\n", err)
		return false
	}

	return muted
}

func (m *LinuxMediaPlayer) CurrentlyPlaying() *ui.MediaItem {
	if !m.IsPlaying() {
		return nil
	}

	metadata := m.mprisClient.GetMetadata()

	var artists []string
	for _, artist := range metadata["xesam:artist"].Value().([]string) {
		artists = append(artists, artist)
	}
	return &ui.MediaItem{
		ID:           metadata["xesam:url"].Value().(string),
		Title:        metadata["xesam:title"].Value().(string),
		Artists:      artists,
		AlbumName:    metadata["xesam:album"].Value().(string),
		AlburmArtURL: metadata["mpris:artUrl"].Value().(string),
	}
}
