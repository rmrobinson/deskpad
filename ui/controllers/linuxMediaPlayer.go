package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus"
	"github.com/lawl/pulseaudio"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/go-mpris"
)

const linuxMediaPlayerVolumeStep = 0.05

// linuxMediaPlayerSeekStepMicros is how far fast-forward and rewind move, in the microseconds MPRIS uses.
const linuxMediaPlayerSeekStepMicros = int64(10 * time.Second / time.Microsecond)

const (
	// linuxMediaPlayerCallTimeout bounds every call to a player so one that has stopped answering can't block the
	// key press handler or state polling. A healthy player replies in milliseconds.
	linuxMediaPlayerCallTimeout = 2 * time.Second
	// linuxMediaPlayerLaunchCheckDelay is how long after launching a playlist the player is checked, purely so
	// that a launch which didn't produce playback leaves a line in the log.
	linuxMediaPlayerLaunchCheckDelay = 5 * time.Second

	mprisBusNamePrefix   = "org.mpris.MediaPlayer2."
	mprisObjectPath      = "/org/mpris/MediaPlayer2"
	mprisPlayerInterface = "org.mpris.MediaPlayer2.Player"
)

// ConnectPlayer starts a Spotify context on a named Spotify Connect device.
type ConnectPlayer interface {
	PlayContext(ctx context.Context, deviceName, uri string) error
}

// LinuxMediaPlayerOption customises a LinuxMediaPlayer.
type LinuxMediaPlayerOption func(*LinuxMediaPlayer)

// WithMPRISNamePrefix restricts the player to MPRIS instances whose bus name starts with prefix, e.g.
// "org.mpris.MediaPlayer2.spotifyd", so that other players on the session bus are never controlled.
func WithMPRISNamePrefix(prefix string) LinuxMediaPlayerOption {
	return func(m *LinuxMediaPlayer) { m.namePrefix = prefix }
}

// WithSpotifyConnect makes PlayURI launch playlists on the named Spotify Connect device through Spotify instead of
// asking the MPRIS player to open the URI. If that call fails PlayURI fails; it does not fall back to MPRIS.
func WithSpotifyConnect(player ConnectPlayer, deviceName string) LinuxMediaPlayerOption {
	return func(m *LinuxMediaPlayer) {
		m.connect = player
		m.connectDevice = deviceName
	}
}

// LinuxMediaPlayer uses the DBus MPRIS interface to control a media player running on the local machine, and
// PulseAudio to control the output volume. It implements the MediaPlayerController interface.
//
// Starting a playlist is the one thing MPRIS can't do reliably: a Spotify Connect receiver such as spotifyd only
// exposes MPRIS once it is the active device, and a third-party player asked to open a playlist URI makes its own
// calls to the Spotify API. With WithSpotifyConnect that step is done through Spotify instead, and MPRIS is used
// for everything after it.
type LinuxMediaPlayer struct {
	lock        sync.Mutex
	mprisConn   *dbus.Conn
	currentName string

	// namePrefix, if set, restricts which MPRIS instances are considered.
	namePrefix string
	// connect, if set, is used by PlayURI to launch playlists on connectDevice.
	connect       ConnectPlayer
	connectDevice string

	paClient *pulseaudio.Client
}

// NewLinuxMediaPlayer creates a new player using the supplied D-Bus session connection and PulseAudio client. The
// MPRIS player to control is looked up when it is first needed, so it doesn't have to be running yet.
func NewLinuxMediaPlayer(mprisConn *dbus.Conn, paClient *pulseaudio.Client, opts ...LinuxMediaPlayerOption) *LinuxMediaPlayer {
	m := &LinuxMediaPlayer{
		mprisConn: mprisConn,
		paClient:  paClient,
	}
	for _, opt := range opts {
		opt(m)
	}

	return m
}

func (m *LinuxMediaPlayer) ID() string {
	name, ok := m.currentPlayerName()
	if !ok {
		return ""
	}

	return name
}

func (m *LinuxMediaPlayer) Play() {
	m.command("Play")
}

func (m *LinuxMediaPlayer) Pause() {
	m.command("Pause")
}

func (m *LinuxMediaPlayer) Next() {
	m.command("Next")
}

func (m *LinuxMediaPlayer) Previous() {
	m.command("Previous")
}

func (m *LinuxMediaPlayer) command(method string) {
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	log.Printf("mpris %s: %s\n", name, method)
	if err := m.callPlayer(name, method); err != nil {
		log.Printf("mpris %s: %s failed: %s\n", name, method, err.Error())
	}
}

func (m *LinuxMediaPlayer) FastForward() {
	m.seek("fast-forward", linuxMediaPlayerSeekStepMicros)
}

func (m *LinuxMediaPlayer) Rewind() {
	m.seek("rewind", -linuxMediaPlayerSeekStepMicros)
}

// seek moves the playback position by a relative offset. MPRIS Seek takes microseconds relative to the current
// position, and clamps to the start of the track, so no position needs to be read first.
func (m *LinuxMediaPlayer) seek(what string, offsetMicros int64) {
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	log.Printf("mpris %s: Seek %s by %dus\n", name, what, offsetMicros)
	if err := m.callPlayer(name, "Seek", offsetMicros); err != nil {
		log.Printf("mpris %s: Seek failed: %s\n", name, err.Error())
	}
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
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	log.Printf("mpris %s: SetShuffle %t\n", name, shuffle)
	ctx, cancel := context.WithTimeout(context.Background(), linuxMediaPlayerCallTimeout)
	defer cancel()

	obj := m.mprisConn.Object(name, dbus.ObjectPath(mprisObjectPath))
	err := callMPRISWithContext(ctx, obj, "org.freedesktop.DBus.Properties.Set", mprisPlayerInterface, "Shuffle", dbus.MakeVariant(shuffle))
	if err != nil {
		log.Printf("mpris %s: error setting shuffle: %s\n", name, err.Error())
	}
}

func (m *LinuxMediaPlayer) IsPlaying() bool {
	name, ok := m.currentPlayerName()
	if !ok {
		return false
	}

	return m.isPlaying(name)
}

func (m *LinuxMediaPlayer) isPlaying(name string) bool {
	status, err := m.playbackStatus(name)
	if err != nil {
		log.Printf("mpris %s: unable to get playback status: %s\n", name, err.Error())
		return false
	}

	return status == string(mpris.PlaybackPlaying)
}

func (m *LinuxMediaPlayer) IsShuffle() bool {
	name, ok := m.currentPlayerName()
	if !ok {
		return false
	}

	variant, err := m.playerProperty(name, "Shuffle")
	if err != nil {
		log.Printf("mpris %s: unable to get shuffle: %s\n", name, err.Error())
		return false
	}

	shuffle, _ := variant.Value().(bool)
	return shuffle
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
	name, ok := m.currentPlayerName()
	if !ok || !m.isPlaying(name) {
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
	variant, err := m.playerProperty(name, "Metadata")
	if err != nil {
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

// PlayURI starts the supplied playlist/album/track URI. With Spotify Connect configured this is done through
// Spotify and nothing is asked of the MPRIS player; otherwise the MPRIS player is asked to open the URI.
func (m *LinuxMediaPlayer) PlayURI(ctx context.Context, uri string) error {
	if m.connect != nil {
		return m.launchViaConnect(ctx, uri)
	}

	name, ok := m.currentPlayerName()
	if !ok {
		return errors.New("no MPRIS media player available")
	}

	log.Printf("mpris %s: OpenUri %s\n", name, uri)
	obj := m.mprisConn.Object(name, dbus.ObjectPath(mprisObjectPath))
	if err := callMPRISWithContext(ctx, obj, mprisPlayerInterface+".OpenUri", uri); err != nil {
		return fmt.Errorf("open URI: %w", err)
	}
	if err := callMPRISWithContext(ctx, obj, mprisPlayerInterface+".Play"); err != nil {
		return fmt.Errorf("play: %w", err)
	}

	return nil
}

func (m *LinuxMediaPlayer) launchViaConnect(ctx context.Context, uri string) error {
	log.Printf("spotify connect %q: play %s\n", m.connectDevice, uri)
	if err := m.connect.PlayContext(ctx, m.connectDevice, uri); err != nil {
		return fmt.Errorf("spotify connect play: %w", err)
	}

	time.AfterFunc(linuxMediaPlayerLaunchCheckDelay, m.logIfNotPlaying)
	return nil
}

// logIfNotPlaying records a line if the player isn't playing shortly after a playlist was launched. It is only
// diagnostic: Spotify accepting the launch doesn't prove the receiver started, and this makes the difference visible.
func (m *LinuxMediaPlayer) logIfNotPlaying() {
	name, ok := m.currentPlayerName()
	if !ok {
		log.Printf("mpris: no media player found %s after launching a playlist\n", linuxMediaPlayerLaunchCheckDelay)
		return
	}

	status, err := m.playbackStatus(name)
	switch {
	case err != nil:
		log.Printf("mpris %s: unable to confirm playback %s after launching a playlist: %s\n", name, linuxMediaPlayerLaunchCheckDelay, err.Error())
	case status == string(mpris.PlaybackStopped):
		log.Printf("mpris %s: still stopped %s after launching a playlist\n", name, linuxMediaPlayerLaunchCheckDelay)
	}
}

// currentPlayerName returns the MPRIS instance to talk to, looked up on the bus each time so a player that has
// restarted under a new name, or that only appeared after startup, is picked up. The current instance is kept while
// it is still on the bus.
func (m *LinuxMediaPlayer) currentPlayerName() (string, bool) {
	names, err := m.listPlayers()
	if err != nil {
		log.Printf("mpris: unable to list media players: %s\n", err.Error())
		return "", false
	}
	names = filterMPRISNames(names, m.namePrefix)

	m.lock.Lock()
	defer m.lock.Unlock()

	chosen := choosePlayer(names, m.currentName)
	if chosen != m.currentName {
		switch {
		case chosen == "":
			log.Printf("mpris: media player %q is no longer available\n", m.currentName)
		case m.currentName == "":
			log.Printf("mpris: media player %q found\n", chosen)
		default:
			log.Printf("mpris: media player %q is no longer available; switched to %q\n", m.currentName, chosen)
		}
		m.currentName = chosen
	}

	return chosen, chosen != ""
}

// listPlayers returns the MPRIS players on the session bus, giving up after linuxMediaPlayerCallTimeout.
func (m *LinuxMediaPlayer) listPlayers() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), linuxMediaPlayerCallTimeout)
	defer cancel()

	call := m.mprisConn.BusObject().Go("org.freedesktop.DBus.ListNames", 0, make(chan *dbus.Call, 1))

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case call := <-call.Done:
		var all []string
		if err := call.Store(&all); err != nil {
			return nil, err
		}

		var players []string
		for _, name := range all {
			if strings.HasPrefix(name, mprisBusNamePrefix) {
				players = append(players, name)
			}
		}
		return players, nil
	}
}

// choosePlayer keeps the current player while it is present, otherwise takes the first available one.
func choosePlayer(names []string, current string) string {
	for _, name := range names {
		if name == current {
			return current
		}
	}

	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// filterMPRISNames returns the names which start with prefix, or all of them if prefix is empty.
func filterMPRISNames(names []string, prefix string) []string {
	if prefix == "" {
		return names
	}

	filtered := make([]string, 0, len(names))
	for _, name := range names {
		if strings.HasPrefix(name, prefix) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

// callPlayer invokes a method on the MPRIS player interface, giving up after linuxMediaPlayerCallTimeout.
func (m *LinuxMediaPlayer) callPlayer(name, method string, args ...interface{}) error {
	ctx, cancel := context.WithTimeout(context.Background(), linuxMediaPlayerCallTimeout)
	defer cancel()

	obj := m.mprisConn.Object(name, dbus.ObjectPath(mprisObjectPath))
	return callMPRISWithContext(ctx, obj, mprisPlayerInterface+"."+method, args...)
}

// playerProperty reads a property of the MPRIS player interface, giving up after linuxMediaPlayerCallTimeout.
func (m *LinuxMediaPlayer) playerProperty(name, property string) (dbus.Variant, error) {
	ctx, cancel := context.WithTimeout(context.Background(), linuxMediaPlayerCallTimeout)
	defer cancel()

	obj := m.mprisConn.Object(name, dbus.ObjectPath(mprisObjectPath))
	call := obj.Go("org.freedesktop.DBus.Properties.Get", 0, make(chan *dbus.Call, 1), mprisPlayerInterface, property)

	select {
	case <-ctx.Done():
		return dbus.Variant{}, ctx.Err()
	case call := <-call.Done:
		var variant dbus.Variant
		if err := call.Store(&variant); err != nil {
			return dbus.Variant{}, err
		}
		return variant, nil
	}
}

func (m *LinuxMediaPlayer) playbackStatus(name string) (string, error) {
	variant, err := m.playerProperty(name, "PlaybackStatus")
	if err != nil {
		return "", err
	}

	status, ok := variant.Value().(string)
	if !ok {
		return "", fmt.Errorf("playback status had unexpected type %T", variant.Value())
	}
	return status, nil
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
