package controllers

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/godbus/dbus"
	"github.com/godbus/dbus/prop"
)

// These tests exercise hung-player detection against a real D-Bus session bus using fake MPRIS players. They are
// skipped when no session bus is reachable (e.g. CI, macOS).

const fakeMPRISPlayerInterface = "org.mpris.MediaPlayer2.Player"

var fakePlayerSeq int32

// fakeMPRISPlayer answers Play/Pause on the bus. onPlay decides what status Play leaves the player in; a "hung"
// player acknowledges the call but doesn't change state, like the wedged ncspot did.
type fakeMPRISPlayer struct {
	props  *prop.Properties
	onPlay func(*prop.Properties)
}

func (f *fakeMPRISPlayer) Play() *dbus.Error {
	if f.onPlay != nil {
		f.onPlay(f.props)
	}
	return nil
}

func (f *fakeMPRISPlayer) Pause() *dbus.Error {
	f.props.SetMust(fakeMPRISPlayerInterface, "PlaybackStatus", "Paused")
	return nil
}

func (f *fakeMPRISPlayer) Next() *dbus.Error     { return nil }
func (f *fakeMPRISPlayer) Previous() *dbus.Error { return nil }

func startFakeMPRISPlayer(t *testing.T, onPlay func(*prop.Properties)) (string, *prop.Properties) {
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
			"PlaybackStatus": {Value: "Stopped", Writable: false, Emit: prop.EmitTrue},
		},
	})

	fake := &fakeMPRISPlayer{props: props, onPlay: onPlay}
	if err := conn.Export(fake, "/org/mpris/MediaPlayer2", fakeMPRISPlayerInterface); err != nil {
		t.Fatalf("export fake player: %s", err)
	}

	name := fmt.Sprintf("org.mpris.MediaPlayer2.deskpadtest.p%d_%d", os.Getpid(), atomic.AddInt32(&fakePlayerSeq, 1))
	reply, err := conn.RequestName(name, 0)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("request name %s: reply %v, err %v", name, reply, err)
	}

	return name, props
}

func newTestController(t *testing.T, name string) *LinuxMediaPlayer {
	t.Helper()

	conn, err := dbus.SessionBus()
	if err != nil {
		t.Skipf("no D-Bus session bus: %s", err)
	}

	m := NewLinuxMediaPlayer(conn, name, nil)
	t.Cleanup(m.Close)
	return m
}

func (m *LinuxMediaPlayer) isFlagged(name string) bool {
	m.lock.Lock()
	defer m.lock.Unlock()

	_, ok := m.unresponsive[name]
	return ok
}

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

func setStatus(status string) func(*prop.Properties) {
	return func(p *prop.Properties) {
		p.SetMust(fakeMPRISPlayerInterface, "PlaybackStatus", status)
	}
}

// settle is long enough for the post-play verification to have run.
const verifySettle = linuxMediaPlayerPlayVerifyDelay + 2*time.Second

func TestPlayVerificationDoesNotFlagPlayingPlayer(t *testing.T) {
	t.Parallel()

	name, _ := startFakeMPRISPlayer(t, setStatus("Playing"))
	m := newTestController(t, name)

	m.Play()
	time.Sleep(verifySettle)

	if m.isFlagged(name) {
		t.Errorf("player that started playing was flagged unresponsive")
	}
}

func TestPlayVerificationFlagsHungPlayerAndFailsOver(t *testing.T) {
	t.Parallel()

	hung, props := startFakeMPRISPlayer(t, nil) // accepts Play, stays Stopped
	healthy, _ := startFakeMPRISPlayer(t, setStatus("Playing"))
	m := newTestController(t, hung)

	m.Play()
	waitFor(t, verifySettle, "hung player to be flagged", func() bool { return m.isFlagged(hung) })

	// Reads must not flip control to another application.
	if got, ok := m.currentPlayerName(); !ok || got != hung {
		t.Errorf("state read moved to %q (ok=%t), want to stay on %q", got, ok, hung)
	}

	// A play command moves away from the flagged player. Other players on the bus (e.g. a real one on a
	// developer machine) are acceptable targets; the flagged one is not.
	if got, ok := m.playerNameForPlay(); !ok || got == hung {
		t.Errorf("play command stayed on flagged player %q (ok=%t); healthy fake is %q", got, ok, healthy)
	}

	// The player recovering announces itself via PropertiesChanged, which clears the flag without polling.
	props.SetMust(fakeMPRISPlayerInterface, "PlaybackStatus", "Playing")
	waitFor(t, 3*time.Second, "flag to clear on Playing signal", func() bool { return !m.isFlagged(hung) })
}

func TestPlayVerificationIgnoresPausedPlayer(t *testing.T) {
	t.Parallel()

	name, _ := startFakeMPRISPlayer(t, setStatus("Paused"))
	m := newTestController(t, name)

	m.Play()
	time.Sleep(verifySettle)

	if m.isFlagged(name) {
		t.Errorf("paused player was flagged unresponsive")
	}
}

func TestPauseCancelsPendingPlayVerification(t *testing.T) {
	t.Parallel()

	name, _ := startFakeMPRISPlayer(t, nil) // would be flagged if the check ran
	m := newTestController(t, name)

	m.Play()
	m.Pause()
	time.Sleep(verifySettle)

	if m.isFlagged(name) {
		t.Errorf("player was flagged although the user paused before the check")
	}
}

func TestPlayOnFrozenPlayerReturnsAndFlagsImmediately(t *testing.T) {
	t.Parallel()

	// The handler never answers within the call timeout, like a process that is stopped or deadlocked.
	frozen, _ := startFakeMPRISPlayer(t, func(*prop.Properties) { time.Sleep(3 * linuxMediaPlayerCallTimeout) })
	m := newTestController(t, frozen)

	start := time.Now()
	m.Play()
	elapsed := time.Since(start)

	if elapsed > linuxMediaPlayerCallTimeout+time.Second {
		t.Errorf("Play blocked for %s on a frozen player, want it bounded by %s", elapsed, linuxMediaPlayerCallTimeout)
	}
	if !m.isFlagged(frozen) {
		t.Errorf("frozen player was not flagged unresponsive after its Play call timed out")
	}
}
