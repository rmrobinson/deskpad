package controllers

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/rmrobinson/house/api/command"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
)

// fakeBridgeMedia is a minimal BridgeMediaPlayerController that records which
// methods Bridge.apply dispatched to, so tests can catch a command routed to
// the wrong underlying call (e.g. SkipForward wired to Previous).
type fakeBridgeMedia struct {
	nextCalls int
	prevCalls int
	nextErr   error
	prevErr   error
}

func (f *fakeBridgeMedia) Play()                           {}
func (f *fakeBridgeMedia) Pause()                          {}
func (f *fakeBridgeMedia) FastForward()                    {}
func (f *fakeBridgeMedia) Rewind()                         {}
func (f *fakeBridgeMedia) Mute()                           {}
func (f *fakeBridgeMedia) Unmute()                         {}
func (f *fakeBridgeMedia) IsPlaying() bool                 { return false }
func (f *fakeBridgeMedia) IsMuted() bool                   { return false }
func (f *fakeBridgeMedia) GetVolume() int                  { return 0 }
func (f *fakeBridgeMedia) SetVolume(pct int)               {}
func (f *fakeBridgeMedia) CurrentlyPlaying() *ui.MediaItem { return nil }

func (f *fakeBridgeMedia) Next() error {
	f.nextCalls++
	return f.nextErr
}

func (f *fakeBridgeMedia) Previous() error {
	f.prevCalls++
	return f.prevErr
}

// fakeBridgeScreen is a no-op deskpad.Screen, just enough to construct a Deck.
type fakeBridgeScreen struct{ name string }

func (s *fakeBridgeScreen) Name() string        { return s.name }
func (s *fakeBridgeScreen) Show() []image.Image { return nil }
func (s *fakeBridgeScreen) Icon() image.Image   { return nil }
func (s *fakeBridgeScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	return deskpad.KeyPressAction{Action: deskpad.KeyPressActionNoop}, nil
}

func newTestBridge(media *fakeBridgeMedia) *Bridge {
	home := &fakeBridgeScreen{name: "home"}
	deck := deskpad.NewDeck(home)
	return NewBridge("device-1", "Test Device", deck, []deskpad.Screen{home}, media)
}

func skipForwardCommand() *command.Command {
	return &command.Command{
		DeviceId: "device-1",
		Details:  &command.Command_SkipForward{SkipForward: &command.SkipForward{}},
	}
}

func skipBackwardCommand() *command.Command {
	return &command.Command{
		DeviceId: "device-1",
		Details:  &command.Command_SkipBackward{SkipBackward: &command.SkipBackward{}},
	}
}

func TestBridgeSkipForwardCallsNextOnly(t *testing.T) {
	media := &fakeBridgeMedia{}
	b := newTestBridge(media)

	d, err := b.ExecuteCommand(context.Background(), skipForwardCommand())
	if err != nil {
		t.Fatalf("ExecuteCommand returned error: %s", err)
	}
	if media.nextCalls != 1 {
		t.Errorf("nextCalls = %d, want 1", media.nextCalls)
	}
	if media.prevCalls != 0 {
		t.Errorf("prevCalls = %d, want 0 (SkipForward must not call Previous)", media.prevCalls)
	}
	if d.GetVersion() != "1" {
		t.Errorf("version = %q, want %q (a successful command must bump the device version)", d.GetVersion(), "1")
	}
}

func TestBridgeSkipBackwardCallsPreviousOnly(t *testing.T) {
	media := &fakeBridgeMedia{}
	b := newTestBridge(media)

	_, err := b.ExecuteCommand(context.Background(), skipBackwardCommand())
	if err != nil {
		t.Fatalf("ExecuteCommand returned error: %s", err)
	}
	if media.prevCalls != 1 {
		t.Errorf("prevCalls = %d, want 1", media.prevCalls)
	}
	if media.nextCalls != 0 {
		t.Errorf("nextCalls = %d, want 0 (SkipBackward must not call Next)", media.nextCalls)
	}
}

func TestBridgeSkipForwardPropagatesFailure(t *testing.T) {
	media := &fakeBridgeMedia{nextErr: errors.New("no MPRIS media player available")}
	b := newTestBridge(media)

	_, err := b.ExecuteCommand(context.Background(), skipForwardCommand())
	if err == nil {
		t.Fatal("ExecuteCommand returned nil error, want the skip failure to be surfaced to the caller")
	}
	if got := status.Code(err); got != codes.Unavailable {
		t.Errorf("status code = %s, want %s", got, codes.Unavailable)
	}
	if v := b.currentVersion(); v != "0" {
		t.Errorf("version = %q, want %q (a failed command must not bump the device version)", v, "0")
	}
}

func TestBridgeSkipBackwardPropagatesFailure(t *testing.T) {
	media := &fakeBridgeMedia{prevErr: errors.New("no MPRIS media player available")}
	b := newTestBridge(media)

	_, err := b.ExecuteCommand(context.Background(), skipBackwardCommand())
	if err == nil {
		t.Fatal("ExecuteCommand returned nil error, want the skip failure to be surfaced to the caller")
	}
	if got := status.Code(err); got != codes.Unavailable {
		t.Errorf("status code = %s, want %s", got, codes.Unavailable)
	}
	if v := b.currentVersion(); v != "0" {
		t.Errorf("version = %q, want %q (a failed command must not bump the device version)", v, "0")
	}
}
