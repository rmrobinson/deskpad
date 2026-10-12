package controllers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/godbus/dbus"
	"github.com/lawl/pulseaudio"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/go-mpris"
)

const linuxMediaPlayerVolumeStep = 0.05

const (
	// linuxMediaPlayerPlayVerifyDelay is how long after a play command we wait before checking that the player
	// actually started playing. A player that has accepted the command on the bus but is not playing is treated
	// as unresponsive.
	linuxMediaPlayerPlayVerifyDelay = 5 * time.Second
	// linuxMediaPlayerStatusTimeout bounds the playback status query so a hung player can't wedge the check.
	linuxMediaPlayerStatusTimeout = 2 * time.Second
	// linuxMediaPlayerUnresponsiveTTL bounds how long a player stays flagged unresponsive if it never signals
	// that it has started playing again.
	linuxMediaPlayerUnresponsiveTTL = 5 * time.Minute

	mprisObjectPath       = "/org/mpris/MediaPlayer2"
	mprisPlayerInterface  = "org.mpris.MediaPlayer2.Player"
	dbusPropertiesChanged = "org.freedesktop.DBus.Properties.PropertiesChanged"
	mprisSignalMatchRule  = "type='signal',interface='org.freedesktop.DBus.Properties',member='PropertiesChanged',path='" + mprisObjectPath + "'"
)

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
	// unresponsive holds MPRIS instance names which accepted a play command but did not start playing, and when
	// they were flagged. When issuing a play command other players are preferred over these. A flag is cleared
	// when the player is seen playing (signal or poll), when it leaves the bus, or after
	// linuxMediaPlayerUnresponsiveTTL.
	unresponsive map[string]time.Time
	// signals receives MPRIS PropertiesChanged signals; it is nil if the listener couldn't be started.
	signals chan *dbus.Signal
	// commandSeq is bumped by commands that change the intent of a pending play verification (e.g. pause).
	commandSeq uint64

	paClient *pulseaudio.Client

	// TODO: add Bluetooth client
}

// NewLinuxMediaPlayer creates a new player using the supplied MRPIS and PulseAudio clients
func NewLinuxMediaPlayer(mprisConn *dbus.Conn, mprisInstanceName string, paClient *pulseaudio.Client) *LinuxMediaPlayer {
	m := &LinuxMediaPlayer{
		mprisConn:         mprisConn,
		mprisClient:       mpris.New(mprisConn, mprisInstanceName),
		mprisInstanceName: mprisInstanceName,
		unresponsive:      make(map[string]time.Time),
		paClient:          paClient,
	}
	m.startSignalListener()
	return m
}

// Close stops listening for MPRIS signals. The supplied D-Bus connection is left open for the caller to close.
func (m *LinuxMediaPlayer) Close() {
	m.lock.Lock()
	ch := m.signals
	m.signals = nil
	m.lock.Unlock()

	if ch == nil {
		return
	}

	m.mprisConn.RemoveSignal(ch)
	m.mprisConn.BusObject().Call("org.freedesktop.DBus.RemoveMatch", 0, mprisSignalMatchRule)
	close(ch)
}

func (m *LinuxMediaPlayer) ID() string {
	_, name, ok := m.currentMPRISClient()
	if !ok {
		return ""
	}

	return name
}

func (m *LinuxMediaPlayer) Play() {
	client, name, ok := m.clientForPlay()
	if !ok {
		return
	}

	log.Printf("mpris %s: Play\n", name)
	seq := m.noteCommand()
	client.Play()
	m.verifyPlaying(name, seq)
}
func (m *LinuxMediaPlayer) Pause() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: Pause\n", name)
	m.noteCommand()
	client.Pause()
}
func (m *LinuxMediaPlayer) Next() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: Next\n", name)
	m.noteCommand()
	client.Next()
}
func (m *LinuxMediaPlayer) Previous() {
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return
	}

	log.Printf("mpris %s: Previous\n", name)
	m.noteCommand()
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
	m.noteCommand()
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
	m.noteCommand()
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
	client, name, ok := m.currentMPRISClient()
	if !ok {
		return false
	}

	status := client.GetPlaybackStatus()
	if status == mpris.PlaybackPlaying {
		m.markResponsive(name)
	}

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
	_, name, ok := m.clientForPlay()
	if !ok {
		return errors.New("no MPRIS media player available")
	}
	seq := m.noteCommand()

	log.Printf("mpris %s: OpenUri %s\n", name, uri)
	obj := m.mprisConn.Object(name, dbus.ObjectPath("/org/mpris/MediaPlayer2"))
	if err := callMPRISWithContext(ctx, obj, "org.mpris.MediaPlayer2.Player.OpenUri", uri); err != nil {
		return fmt.Errorf("open URI: %w", err)
	}
	if err := callMPRISWithContext(ctx, obj, "org.mpris.MediaPlayer2.Player.Play"); err != nil {
		return fmt.Errorf("play: %w", err)
	}
	m.verifyPlaying(name, seq)

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

// noteCommand records that a command affecting playback intent was issued, and returns the new sequence number.
func (m *LinuxMediaPlayer) noteCommand() uint64 {
	m.lock.Lock()
	defer m.lock.Unlock()

	m.commandSeq++
	return m.commandSeq
}

// verifyPlaying checks, after linuxMediaPlayerPlayVerifyDelay, that the named player started playing. If another
// playback-affecting command was issued in the meantime the check is skipped, as the outcome is no longer meaningful.
// A player which is still Stopped (or can't be queried) is flagged as unresponsive so another instance is preferred
// on the next play command. Any other status, such as Paused, means the player is handling commands.
func (m *LinuxMediaPlayer) verifyPlaying(name string, seq uint64) {
	time.AfterFunc(linuxMediaPlayerPlayVerifyDelay, func() {
		m.lock.Lock()
		stale := m.commandSeq != seq
		m.lock.Unlock()
		if stale {
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), linuxMediaPlayerStatusTimeout)
		defer cancel()

		status, err := m.playbackStatus(ctx, name)
		switch {
		case err == nil && status == string(mpris.PlaybackPlaying):
			m.markResponsive(name)
			return
		case err == nil && status != string(mpris.PlaybackStopped):
			// Paused (e.g. by the user in the player itself); it is responding.
			return
		case err != nil:
			log.Printf("mpris %s: no playback %s after play command (status query failed: %s); marking unresponsive\n", name, linuxMediaPlayerPlayVerifyDelay, err.Error())
		default:
			log.Printf("mpris %s: no playback %s after play command (status %q); marking unresponsive\n", name, linuxMediaPlayerPlayVerifyDelay, status)
		}

		m.lock.Lock()
		m.unresponsive[name] = time.Now()
		m.lock.Unlock()
	})
}

func (m *LinuxMediaPlayer) markResponsive(name string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.unresponsive[name]; ok {
		log.Printf("mpris %s: playing again; no longer marked unresponsive\n", name)
		delete(m.unresponsive, name)
	}
}

func (m *LinuxMediaPlayer) playbackStatus(ctx context.Context, name string) (string, error) {
	obj := m.mprisConn.Object(name, dbus.ObjectPath(mprisObjectPath))
	call := obj.Go("org.freedesktop.DBus.Properties.Get", 0, make(chan *dbus.Call, 1), mprisPlayerInterface, "PlaybackStatus")

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case call := <-call.Done:
		var variant dbus.Variant
		if err := call.Store(&variant); err != nil {
			return "", err
		}
		status, ok := variant.Value().(string)
		if !ok {
			return "", fmt.Errorf("playback status had unexpected type %T", variant.Value())
		}
		return status, nil
	}
}

// startSignalListener subscribes to MPRIS PropertiesChanged signals so that a player flagged as unresponsive is
// restored as soon as it reports that it is playing, without needing to be polled.
func (m *LinuxMediaPlayer) startSignalListener() {
	if m.mprisConn == nil {
		return
	}

	if err := m.mprisConn.BusObject().Call("org.freedesktop.DBus.AddMatch", 0, mprisSignalMatchRule).Err; err != nil {
		log.Printf("mpris: unable to listen for player signals: %s\n", err.Error())
		return
	}

	ch := make(chan *dbus.Signal, 32)
	m.signals = ch
	m.mprisConn.Signal(ch)

	go func() {
		for sig := range ch {
			if status, ok := playbackStatusFromSignal(sig.Name, sig.Body); ok && status == string(mpris.PlaybackPlaying) {
				m.markSenderResponsive(sig.Sender)
			}
		}
	}()
}

// markSenderResponsive clears the unresponsive flag of any flagged instance owned by the given unique bus name.
func (m *LinuxMediaPlayer) markSenderResponsive(sender string) {
	m.lock.Lock()
	flagged := make([]string, 0, len(m.unresponsive))
	for name := range m.unresponsive {
		flagged = append(flagged, name)
	}
	m.lock.Unlock()

	for _, name := range flagged {
		var owner string
		if err := m.mprisConn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, name).Store(&owner); err != nil {
			continue
		}
		if owner == sender {
			m.markResponsive(name)
		}
	}
}

// playbackStatusFromSignal extracts the new PlaybackStatus from a Properties.PropertiesChanged signal for the MPRIS
// player interface. It returns false if the signal is some other signal or doesn't carry a status change.
func playbackStatusFromSignal(name string, body []interface{}) (string, bool) {
	if name != dbusPropertiesChanged || len(body) < 2 {
		return "", false
	}

	iface, ok := body[0].(string)
	if !ok || iface != mprisPlayerInterface {
		return "", false
	}

	changed, ok := body[1].(map[string]dbus.Variant)
	if !ok {
		return "", false
	}

	variant, ok := changed["PlaybackStatus"]
	if !ok {
		return "", false
	}

	status, ok := variant.Value().(string)
	return status, ok
}

// chooseMPRISPlayer picks which MPRIS instance to control from those currently on the bus. The current instance is
// kept while it is present, unless demote is set and it is flagged unresponsive. Otherwise the first responsive other
// instance wins. If every instance is unresponsive the current one is kept (or the first, if the current one is gone)
// so commands still go somewhere. It returns an empty name when no instance is available.
func chooseMPRISPlayer(names []string, current string, unresponsive map[string]time.Time, demote bool) string {
	currentPresent := false
	for _, name := range names {
		if name == current {
			currentPresent = true
			break
		}
	}

	_, currentFlagged := unresponsive[current]
	if currentPresent && (!demote || !currentFlagged) {
		return current
	}

	for _, name := range names {
		if _, flagged := unresponsive[name]; name != current && !flagged {
			return name
		}
	}

	if currentPresent {
		return current
	}
	if len(names) > 0 {
		return names[0]
	}
	return ""
}

// currentMPRISClient returns the player to read state from or send non-play commands to. It only changes player when
// the current one has left the bus, so state polls never flip control to another application.
func (m *LinuxMediaPlayer) currentMPRISClient() (*mpris.Player, string, bool) {
	return m.resolveMPRISClient(false)
}

// clientForPlay returns the player to send a play command to. Unlike currentMPRISClient it will also move away from
// a player which was flagged unresponsive by an earlier play command.
func (m *LinuxMediaPlayer) clientForPlay() (*mpris.Player, string, bool) {
	return m.resolveMPRISClient(true)
}

func (m *LinuxMediaPlayer) resolveMPRISClient(demote bool) (*mpris.Player, string, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	names, err := mpris.List(m.mprisConn)
	if err != nil {
		log.Printf("mpris: unable to list media players: %s\n", err.Error())
		return nil, "", false
	}

	// Forget unresponsive flags for instances that have left the bus or that have expired.
	for name, flaggedAt := range m.unresponsive {
		present := false
		for _, n := range names {
			if n == name {
				present = true
				break
			}
		}
		if !present || time.Since(flaggedAt) > linuxMediaPlayerUnresponsiveTTL {
			delete(m.unresponsive, name)
		}
	}

	chosen := chooseMPRISPlayer(names, m.mprisInstanceName, m.unresponsive, demote)
	if chosen != "" && chosen == m.mprisInstanceName && m.mprisClient != nil {
		return m.mprisClient, chosen, true
	}

	if chosen == "" {
		if m.mprisInstanceName != "" {
			log.Printf("mpris: media player %q is no longer available and no replacement was found\n", m.mprisInstanceName)
		}
		m.mprisClient = nil
		m.mprisInstanceName = ""
		return nil, "", false
	}

	oldName := m.mprisInstanceName
	m.mprisInstanceName = chosen
	m.mprisClient = mpris.New(m.mprisConn, chosen)
	if oldName == "" {
		log.Printf("mpris: media player %q found\n", chosen)
	} else {
		log.Printf("mpris: switching media player from %q to %q\n", oldName, chosen)
	}
	return m.mprisClient, chosen, true
}
