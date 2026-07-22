package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"

	"github.com/godbus/dbus"
	"github.com/lawl/pulseaudio"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/go-mpris"
)

const linuxMediaPlayerVolumeStep = 0.05

// LinuxMediaPlayer uses the DBus MPRIS interface to control a media agent running on the local machine,
// and PulseAudio to control the output audio device settings.
// This supports the MediaPlayerController interface for audio control. It currently doesn't support listing
// media playlists - paired with a suitable MPRIS agent and the Spotify media player it can play the playlist
// URIs returned by the Spotify player playlist functions.
type LinuxMediaPlayer struct {
	lock              sync.Mutex
	mprisConn         *dbus.Conn
	mprisClient       *mpris.Player
	mprisInstanceName string

	paClient *pulseaudio.Client

	// TODO: add Bluetooth client
}

// NewLinuxMediaPlayer creates a new player using the supplied MRPIS and PulseAudio clients
func NewLinuxMediaPlayer(mprisConn *dbus.Conn, mprisInstanceName string, paClient *pulseaudio.Client) *LinuxMediaPlayer {
	return &LinuxMediaPlayer{
		mprisConn:         mprisConn,
		mprisClient:       mpris.New(mprisConn, mprisInstanceName),
		mprisInstanceName: mprisInstanceName,
		paClient:          paClient,
	}
}

func (m *LinuxMediaPlayer) ID() string {
	_, name, ok := m.currentMPRISClient()
	if !ok {
		return ""
	}

	return name
}

func (m *LinuxMediaPlayer) Play() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: Play\n", name)
	client.Play()
}
func (m *LinuxMediaPlayer) Pause() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: Pause\n", name)
	client.Pause()
}
func (m *LinuxMediaPlayer) Next() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: Next\n", name)
	client.Next()
}
func (m *LinuxMediaPlayer) Previous() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: Previous\n", name)
	client.Previous()
}
func (m *LinuxMediaPlayer) FastForward() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	pos := client.GetPosition()

	newPos := pos + 10000
	log.Printf("mpris %s: SeekTo fast-forward from %d to %d\n", name, pos, newPos)
	client.SeekTo(newPos)
}
func (m *LinuxMediaPlayer) Rewind() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	pos := client.GetPosition()

	newPos := pos - 10000
	if newPos < 0 {
		newPos = 0
	}
	log.Printf("mpris %s: SeekTo rewind from %d to %d\n", name, pos, newPos)
	client.SeekTo(newPos)

}
func (m *LinuxMediaPlayer) VolumeUp() {
	v, err := m.paClient.Volume()
	if err != nil {
		log.Printf("error getting volume: %s\n", err.Error())
		return
	}

	v += linuxMediaPlayerVolumeStep
	if v > 1.0 {
		v = 1.0
	}
	m.paClient.SetVolume(v)
}

func (m *LinuxMediaPlayer) VolumeDown() {
	v, err := m.paClient.Volume()
	if err != nil {
		log.Printf("error getting volume: %s\n", err.Error())
		return
	}

	v -= linuxMediaPlayerVolumeStep
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
	_, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: SetShuffle %t\n", name, shuffle)
	obj := m.mprisConn.Object(name, dbus.ObjectPath("/org/mpris/MediaPlayer2"))
	call := obj.Call(
		"org.freedesktop.DBus.Properties.Set",
		0,
		"org.mpris.MediaPlayer2.Player",
		"Shuffle",
		dbus.MakeVariant(shuffle),
	)
	if call.Err != nil {
		log.Printf("mpris %s: error setting shuffle: %s\n", name, call.Err.Error())
	}
}

func (m *LinuxMediaPlayer) IsPlaying() bool {
	client, _, ok := m.currentMPRISClient()
	if !ok {
		return false
	}

	status := client.GetPlaybackStatus()

	return status == mpris.PlaybackPlaying
}
func (m *LinuxMediaPlayer) IsShuffle() bool {
	client, _, ok := m.currentMPRISClient()
	if !ok {
		return false
	}

	status := client.GetShuffle()
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

func (m *LinuxMediaPlayer) CurrentlyPlaying() *ui.MediaItem {
	client, name, ok := m.currentMPRISClient()
	if !ok || client.GetPlaybackStatus() != mpris.PlaybackPlaying {
		return nil
	}

	metadata := m.getMetadata(name)

	return &ui.MediaItem{
		ID:           metadataString(metadata, "xesam:url"),
		Title:        metadataString(metadata, "xesam:title"),
		Artists:      metadataStrings(metadata, "xesam:artist"),
		AlbumName:    metadataString(metadata, "xesam:album"),
		AlburmArtURL: metadataString(metadata, "mpris:artUrl"),
	}
}

func (m *LinuxMediaPlayer) getMetadata(name string) map[string]dbus.Variant {
	obj := m.mprisConn.Object(name, dbus.ObjectPath("/org/mpris/MediaPlayer2"))

	var variant dbus.Variant
	if err := obj.Call("org.freedesktop.DBus.Properties.Get", 0, "org.mpris.MediaPlayer2.Player", "Metadata").Store(&variant); err != nil {
		log.Printf("mpris %s: error getting metadata: %s\n", name, err.Error())
		return nil
	}

	metadata, ok := variant.Value().(map[string]dbus.Variant)
	if !ok {
		log.Printf("mpris %s: metadata had unexpected type %T\n", name, variant.Value())
		return nil
	}

	return metadata
}

func metadataString(metadata map[string]dbus.Variant, key string) string {
	variant, ok := metadata[key]
	if !ok {
		return ""
	}

	value, ok := variant.Value().(string)
	if !ok {
		return ""
	}

	return value
}

func metadataStrings(metadata map[string]dbus.Variant, key string) []string {
	variant, ok := metadata[key]
	if !ok {
		return nil
	}

	switch value := variant.Value().(type) {
	case []string:
		return append([]string(nil), value...)
	case string:
		return []string{value}
	case []interface{}:
		values := make([]string, 0, len(value))
		for _, item := range value {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return values
	default:
		return nil
	}
}

func (m *LinuxMediaPlayer) PlayURI(ctx context.Context, uri string) error {
	_, name, ok := m.currentMPRISClient()
	if !ok {
		return errors.New("no MPRIS media player available")
	}

	log.Printf("mpris %s: OpenUri %s\n", name, uri)
	obj := m.mprisConn.Object(name, dbus.ObjectPath("/org/mpris/MediaPlayer2"))
	if err := callMPRISWithContext(ctx, obj, "org.mpris.MediaPlayer2.Player.OpenUri", uri); err != nil {
		return fmt.Errorf("open URI: %w", err)
	}
	if err := callMPRISWithContext(ctx, obj, "org.mpris.MediaPlayer2.Player.Play"); err != nil {
		return fmt.Errorf("play: %w", err)
	}

	return nil
}

func callMPRISWithContext(ctx context.Context, obj dbus.BusObject, method string, args ...interface{}) error {
	call := obj.Go(method, 0, make(chan *dbus.Call, 1), args...)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case call := <-call.Done:
		return call.Err
	}
}

func (m *LinuxMediaPlayer) currentMPRISClient() (*mpris.Player, string, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	names, err := mpris.List(m.mprisConn)
	if err != nil {
		log.Printf("mpris: unable to list media players: %s\n", err.Error())
		return nil, "", false
	}

	for _, name := range names {
		if name == m.mprisInstanceName {
			return m.mprisClient, m.mprisInstanceName, true
		}
	}

	if len(names) == 0 {
		if m.mprisInstanceName != "" {
			log.Printf("mpris: media player %q is no longer available and no replacement was found\n", m.mprisInstanceName)
		}
		m.mprisClient = nil
		m.mprisInstanceName = ""
		return nil, "", false
	}

	oldName := m.mprisInstanceName
	m.mprisInstanceName = names[0]
	m.mprisClient = mpris.New(m.mprisConn, m.mprisInstanceName)
	log.Printf("mpris: media player %q is no longer available; switched to %q\n", oldName, m.mprisInstanceName)
	return m.mprisClient, m.mprisInstanceName, true
}
