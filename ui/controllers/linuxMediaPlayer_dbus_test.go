package controllers

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/prop"
)

// These tests run fake MPRIS players on a real D-Bus session bus. They are skipped when no session bus is reachable
// (e.g. CI, macOS).

const fakeMPRISPlayerInterface = "org.mpris.MediaPlayer2.Player"

var fakePlayerSeq int32

// fakeMPRISPlayer answers the Player methods on the bus and records what it was asked to do.
type fakeMPRISPlayer struct {
	props  *prop.Properties
	onPlay func()

	openUriCalls int32
	playCalls    int32
	pauseCalls   int32
	seekOffset   int64
	seekCalls    int32
}

// SeekBy is exported on the bus as Seek; the Go name avoids go vet's io.Seeker signature check.
func (f *fakeMPRISPlayer) SeekBy(offset int64) *dbus.Error {
	atomic.StoreInt64(&f.seekOffset, offset)
	atomic.AddInt32(&f.seekCalls, 1)
	return nil
}

func (f *fakeMPRISPlayer) Play() *dbus.Error {
	atomic.AddInt32(&f.playCalls, 1)
	if f.onPlay != nil {
		f.onPlay()
	}
	return nil
}

func (f *fakeMPRISPlayer) Pause() *dbus.Error {
	atomic.AddInt32(&f.pauseCalls, 1)
	return nil
}

func (f *fakeMPRISPlayer) OpenUri(string) *dbus.Error {
	atomic.AddInt32(&f.openUriCalls, 1)
	return nil
}

func (f *fakeMPRISPlayer) Next() *dbus.Error     { return nil }
func (f *fakeMPRISPlayer) Previous() *dbus.Error { return nil }

func startFakeMPRISPlayer(t *testing.T, status string, onPlay func()) (string, *fakeMPRISPlayer) {
	t.Helper()

	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		t.Skipf("no D-Bus session bus: %s", err)
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		t.Skipf("unable to authenticate to D-Bus session bus: %s", err)
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		t.Skipf("unable to say hello to D-Bus session bus: %s", err)
	}
	t.Cleanup(func() { conn.Close() })

	props := prop.New(conn, "/org/mpris/MediaPlayer2", map[string]map[string]*prop.Prop{
		fakeMPRISPlayerInterface: {
			"PlaybackStatus": {Value: status, Writable: false, Emit: prop.EmitTrue},
		},
	})

	fake := &fakeMPRISPlayer{props: props, onPlay: onPlay}
	if err := conn.ExportWithMap(fake, map[string]string{"SeekBy": "Seek"}, "/org/mpris/MediaPlayer2", fakeMPRISPlayerInterface); err != nil {
		t.Fatalf("export fake player: %s", err)
	}

	name := fmt.Sprintf("org.mpris.MediaPlayer2.deskpadtest.p%d_%d", os.Getpid(), atomic.AddInt32(&fakePlayerSeq, 1))
	reply, err := conn.RequestName(name, 0)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name %s: reply %v, err %v", name, reply, err)
	}

	return name, fake
}

func newTestController(t *testing.T, opts ...LinuxMediaPlayerOption) *LinuxMediaPlayer {
	t.Helper()

	conn, err := dbus.SessionBus()
	if err != nil {
		t.Skipf("no D-Bus session bus: %s", err)
	}

	return NewLinuxMediaPlayer(conn, nil, opts...)
}

type connectCall struct {
	device, uri string
}

type recordingConnectPlayer struct {
	mu    sync.Mutex
	calls []connectCall
	err   error
}

func (r *recordingConnectPlayer) PlayContext(_ context.Context, deviceName, uri string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, connectCall{device: deviceName, uri: uri})
	return r.err
}

func (r *recordingConnectPlayer) recorded() []connectCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]connectCall(nil), r.calls...)
}

func TestPlayURILaunchesViaSpotifyConnect(t *testing.T) {
	t.Parallel()

	name, fake := startFakeMPRISPlayer(t, "Stopped", nil)
	connect := &recordingConnectPlayer{}
	m := newTestController(t, WithMPRISNamePrefix(name), WithSpotifyConnect(connect, "deskpad"))

	if err := m.PlayURI(context.Background(), "spotify:playlist:abc"); err != nil {
		t.Fatalf("PlayURI: %s", err)
	}

	calls := connect.recorded()
	if len(calls) != 1 || calls[0] != (connectCall{device: "deskpad", uri: "spotify:playlist:abc"}) {
		t.Errorf("connect calls = %+v, want one launch of spotify:playlist:abc on deskpad", calls)
	}
	if got := atomic.LoadInt32(&fake.openUriCalls); got != 0 {
		t.Errorf("MPRIS OpenUri called %d times, want 0 when Spotify Connect is configured", got)
	}
}

func TestPlayURIDoesNotFallBackWhenConnectFails(t *testing.T) {
	t.Parallel()

	name, fake := startFakeMPRISPlayer(t, "Stopped", nil)
	connect := &recordingConnectPlayer{err: errors.New("device not found")}
	m := newTestController(t, WithMPRISNamePrefix(name), WithSpotifyConnect(connect, "deskpad"))

	if err := m.PlayURI(context.Background(), "spotify:playlist:abc"); err == nil {
		t.Errorf("PlayURI succeeded, want the Spotify Connect error")
	}
	if got := atomic.LoadInt32(&fake.openUriCalls); got != 0 {
		t.Errorf("MPRIS OpenUri called %d times after Connect failed, want 0 (no fallback)", got)
	}
}

func TestPlayURIUsesOpenUriWithoutConnect(t *testing.T) {
	t.Parallel()

	name, fake := startFakeMPRISPlayer(t, "Playing", nil)
	m := newTestController(t, WithMPRISNamePrefix(name))

	if err := m.PlayURI(context.Background(), "spotify:playlist:abc"); err != nil {
		t.Fatalf("PlayURI: %s", err)
	}
	if got := atomic.LoadInt32(&fake.openUriCalls); got != 1 {
		t.Errorf("MPRIS OpenUri called %d times, want 1", got)
	}
}

func TestNamePrefixRestrictsControlledPlayer(t *testing.T) {
	t.Parallel()

	wanted, wantedFake := startFakeMPRISPlayer(t, "Stopped", nil)
	_, otherFake := startFakeMPRISPlayer(t, "Stopped", nil)
	m := newTestController(t, WithMPRISNamePrefix(wanted))

	if got := m.ID(); got != wanted {
		t.Errorf("ID() = %q, want %q", got, wanted)
	}

	m.Play()
	if got := atomic.LoadInt32(&wantedFake.playCalls); got != 1 {
		t.Errorf("matching player got %d Play calls, want 1", got)
	}
	if got := atomic.LoadInt32(&otherFake.playCalls); got != 0 {
		t.Errorf("non-matching player got %d Play calls, want 0", got)
	}
}

func TestPlayerFoundWhenItAppearsAfterStartup(t *testing.T) {
	t.Parallel()

	// The controller is created before any matching player exists on the bus.
	prefix := fmt.Sprintf("org.mpris.MediaPlayer2.deskpadtest.late%d", os.Getpid())
	m := newTestController(t, WithMPRISNamePrefix(prefix))

	if got := m.ID(); got != "" {
		t.Fatalf("ID() = %q before any player exists, want empty", got)
	}
	if m.IsPlaying() {
		t.Errorf("IsPlaying() = true with no player")
	}

	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		t.Skipf("no D-Bus session bus: %s", err)
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		t.Skipf("unable to authenticate: %s", err)
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		t.Skipf("unable to hello: %s", err)
	}
	t.Cleanup(func() { conn.Close() })

	prop.New(conn, "/org/mpris/MediaPlayer2", map[string]map[string]*prop.Prop{
		fakeMPRISPlayerInterface: {"PlaybackStatus": {Value: "Playing", Emit: prop.EmitTrue}},
	})
	late := prefix + ".instance1"
	if reply, err := conn.RequestName(late, 0); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name: reply %v, err %v", reply, err)
	}

	if got := m.ID(); got != late {
		t.Errorf("ID() = %q after the player appeared, want %q", got, late)
	}
	if !m.IsPlaying() {
		t.Errorf("IsPlaying() = false for a player reporting Playing")
	}
}

func TestCommandsOnFrozenPlayerAreBounded(t *testing.T) {
	t.Parallel()

	// Play and Pause are accepted by the bus but the handler never answers within the call timeout.
	block := func() { time.Sleep(3 * linuxMediaPlayerCallTimeout) }
	name, _ := startFakeMPRISPlayer(t, "Stopped", block)
	m := newTestController(t, WithMPRISNamePrefix(name))

	start := time.Now()
	m.Play()
	if elapsed := time.Since(start); elapsed > linuxMediaPlayerCallTimeout+time.Second {
		t.Errorf("Play blocked for %s on a frozen player, want it bounded by %s", elapsed, linuxMediaPlayerCallTimeout)
	}
}

func TestSeekSendsRelativeOffsetInMicroseconds(t *testing.T) {
	t.Parallel()

	name, fake := startFakeMPRISPlayer(t, "Playing", nil)
	m := newTestController(t, WithMPRISNamePrefix(name))

	m.FastForward()
	if got, want := atomic.LoadInt64(&fake.seekOffset), int64(10_000_000); got != want {
		t.Errorf("FastForward sent Seek(%d), want Seek(%d)", got, want)
	}

	m.Rewind()
	if got, want := atomic.LoadInt64(&fake.seekOffset), int64(-10_000_000); got != want {
		t.Errorf("Rewind sent Seek(%d), want Seek(%d)", got, want)
	}
	if got := atomic.LoadInt32(&fake.seekCalls); got != 2 {
		t.Errorf("Seek called %d times, want 2", got)
	}
}
