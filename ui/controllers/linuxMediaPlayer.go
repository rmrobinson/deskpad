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
	// linuxMediaPlayerCallTimeout bounds every call to a player so a frozen player can't block the key press
	// handler or state polling. A responsive player replies in milliseconds.
	linuxMediaPlayerCallTimeout = 2 * time.Second
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
	name, ok := m.currentPlayerName()
	if !ok {
		return ""
	}

	return name
}

func (m *LinuxMediaPlayer) Play() {
	name, ok := m.playerNameForPlay()
	if !ok {
		return
	}

	log.Printf("mpris %s: Play\n", name)
	seq := m.noteCommand()
	if err := m.callPlayer(name, "Play"); err != nil {
		log.Printf("mpris %s: Play failed: %s\n", name, err.Error())
		if errors.Is(err, context.DeadlineExceeded) {
			// A player that doesn't answer a method call at all is as unresponsive as one that ignores it.
			m.markUnresponsive(name, "play command timed out")
		}
		return
	}
	m.verifyPlaying(name, seq)
}
func (m *LinuxMediaPlayer) Pause() {
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	log.Printf("mpris %s: Pause\n", name)
	m.noteCommand()
	if err := m.callPlayer(name, "Pause"); err != nil {
		log.Printf("mpris %s: Pause failed: %s\n", name, err.Error())
	}
}
func (m *LinuxMediaPlayer) Next() {
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	log.Printf("mpris %s: Next\n", name)
	m.noteCommand()
	if err := m.callPlayer(name, "Next"); err != nil {
		log.Printf("mpris %s: Next failed: %s\n", name, err.Error())
	}
}
func (m *LinuxMediaPlayer) Previous() {
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	log.Printf("mpris %s: Previous\n", name)
	m.noteCommand()
	if err := m.callPlayer(name, "Previous"); err != nil {
		log.Printf("mpris %s: Previous failed: %s\n", name, err.Error())
	}
}
func (m *LinuxMediaPlayer) FastForward() {
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	pos, err := m.position(name)
	if err != nil {
		log.Printf("mpris %s: unable to get position: %s\n", name, err.Error())
		return
	}

	newPos := pos + 10000
	log.Printf("mpris %s: SeekTo fast-forward from %d to %d\n", name, pos, newPos)
	m.noteCommand()
	if err := m.callPlayer(name, "Seek", newPos); err != nil {
		log.Printf("mpris %s: Seek failed: %s\n", name, err.Error())
	}
}
func (m *LinuxMediaPlayer) Rewind() {
	name, ok := m.currentPlayerName()
	if !ok {
		return
	}

	pos, err := m.position(name)
	if err != nil {
		log.Printf("mpris %s: unable to get position: %s\n", name, err.Error())
		return
	}

	newPos := pos - 10000
	if newPos < 0 {
		newPos = 0
	}
	log.Printf("mpris %s: SeekTo rewind from %d to %d\n", name, pos, newPos)
	m.noteCommand()
	if err := m.callPlayer(name, "Seek", newPos); err != nil {
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
	obj := m.mprisConn.Object(name, dbus.ObjectPath(mprisObjectPath))
	ctx, cancel := context.WithTimeout(context.Background(), linuxMediaPlayerCallTimeout)
	defer cancel()
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

	ctx, cancel := context.WithTimeout(context.Background(), linuxMediaPlayerStatusTimeout)
	defer cancel()

	status, err := m.playbackStatus(ctx, name)
	if err != nil {
		log.Printf("mpris %s: unable to get playback status: %s\n", name, err.Error())
		return false
	}

	playing := status == string(mpris.PlaybackPlaying)
	if playing {
		m.markResponsive(name)
	}

	return playing
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
	if !ok || !m.IsPlaying() {
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

	return m.playerPropertyContext(ctx, name, property)
}

func (m *LinuxMediaPlayer) playerPropertyContext(ctx context.Context, name, property string) (dbus.Variant, error) {
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

func (m *LinuxMediaPlayer) position(name string) (int64, error) {
	variant, err := m.playerProperty(name, "Position")
	if err != nil {
		return 0, err
	}

	pos, ok := variant.Value().(int64)
	if !ok {
		return 0, fmt.Errorf("position had unexpected type %T", variant.Value())
	}
	return pos, nil
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
	name, ok := m.playerNameForPlay()
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

		m.markUnresponsive(name, "")
	})
}

// markUnresponsive flags the named player so another instance is preferred on the next play command. A non-empty
// reason is logged; callers that have already logged why pass an empty one.
func (m *LinuxMediaPlayer) markUnresponsive(name, reason string) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if reason != "" {
		log.Printf("mpris %s: %s; marking unresponsive\n", name, reason)
	}
	m.unresponsive[name] = time.Now()
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
	variant, err := m.playerPropertyContext(ctx, name, "PlaybackStatus")
	if err != nil {
		return "", err
	}

	status, ok := variant.Value().(string)
	if !ok {
		return "", fmt.Errorf("playback status had unexpected type %T", variant.Value())
	}
	return status, nil
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

// currentPlayerName returns the player to read state from or send non-play commands to. It only changes player when
// the current one has left the bus, so state polls never flip control to another application.
func (m *LinuxMediaPlayer) currentPlayerName() (string, bool) {
	return m.resolvePlayer(false)
}

// playerNameForPlay returns the player to send a play command to. Unlike currentPlayerName it will also move away from
// a player which was flagged unresponsive by an earlier play command.
func (m *LinuxMediaPlayer) playerNameForPlay() (string, bool) {
	return m.resolvePlayer(true)
}

func (m *LinuxMediaPlayer) resolvePlayer(demote bool) (string, bool) {
	m.lock.Lock()
	defer m.lock.Unlock()

	names, err := mpris.List(m.mprisConn)
	if err != nil {
		log.Printf("mpris: unable to list media players: %s\n", err.Error())
		return "", false
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
	if chosen != "" && chosen == m.mprisInstanceName {
		return chosen, true
	}

	if chosen == "" {
		if m.mprisInstanceName != "" {
			log.Printf("mpris: media player %q is no longer available and no replacement was found\n", m.mprisInstanceName)
		}
		m.mprisInstanceName = ""
		return "", false
	}

	oldName := m.mprisInstanceName
	m.mprisInstanceName = chosen
	if oldName == "" {
		log.Printf("mpris: media player %q found\n", chosen)
	} else {
		log.Printf("mpris: switching media player from %q to %q\n", oldName, chosen)
	}
	return chosen, true
}
