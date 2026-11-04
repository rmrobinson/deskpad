package controllers

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/rmrobinson/house/api"
	"github.com/rmrobinson/house/api/command"
	apidevice "github.com/rmrobinson/house/api/device"
	apitrait "github.com/rmrobinson/house/api/trait"
	"google.golang.org/grpc"

	"github.com/rmrobinson/deskpad/ui"
)

// fakeHouseServiceClient implements only the GetRoom method House actually
// calls; every other api.HouseServiceClient method is left nil and must not
// be exercised by the code under test.
type fakeHouseServiceClient struct {
	api.HouseServiceClient

	mu        sync.Mutex
	getRoomFn func(ctx context.Context) (*api.Room, error)
	calls     int
}

func (f *fakeHouseServiceClient) GetRoom(ctx context.Context, in *api.GetRoomRequest, opts ...grpc.CallOption) (*api.Room, error) {
	f.mu.Lock()
	f.calls++
	fn := f.getRoomFn
	f.mu.Unlock()
	return fn(ctx)
}

func (f *fakeHouseServiceClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// fakeBridgeServiceClient implements only StreamUpdates and ExecuteCommand;
// every other api.BridgeServiceClient method is left nil.
type fakeBridgeServiceClient struct {
	api.BridgeServiceClient

	mu               sync.Mutex
	streamFn         func(ctx context.Context) (grpc.ServerStreamingClient[api.Update], error)
	calls            int
	lastCommand      *command.Command
	executeCommandFn func(ctx context.Context, in *command.Command) (*apidevice.Device, error)
}

func (f *fakeBridgeServiceClient) StreamUpdates(ctx context.Context, in *api.StreamUpdatesRequest, opts ...grpc.CallOption) (grpc.ServerStreamingClient[api.Update], error) {
	f.mu.Lock()
	f.calls++
	fn := f.streamFn
	f.mu.Unlock()
	return fn(ctx)
}

func (f *fakeBridgeServiceClient) ExecuteCommand(ctx context.Context, in *command.Command, opts ...grpc.CallOption) (*apidevice.Device, error) {
	f.mu.Lock()
	f.lastCommand = in
	fn := f.executeCommandFn
	f.mu.Unlock()
	if fn != nil {
		return fn(ctx, in)
	}
	return &apidevice.Device{Id: in.GetDeviceId()}, nil
}

func (f *fakeBridgeServiceClient) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

func (f *fakeBridgeServiceClient) getLastCommand() *command.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastCommand
}

// fakeUpdateStream implements grpc.ServerStreamingClient[api.Update]. Recv
// blocks until ctx is cancelled (mimicking a live, otherwise-idle stream),
// optionally sleeping before returning and/or reporting that it returned, so
// tests can tell whether a caller actually waited for it to unblock.
type fakeUpdateStream struct {
	grpc.ClientStream
	ctx      context.Context
	delay    time.Duration
	onReturn func()
}

func (s *fakeUpdateStream) Recv() (*api.Update, error) {
	<-s.ctx.Done()
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	if s.onReturn != nil {
		s.onReturn()
	}
	return nil, s.ctx.Err()
}

func waitOrFatal(t *testing.T, signal <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatal(msg)
	}
}

func testLightDevice(id, name string, on bool) *apidevice.Device {
	return &apidevice.Device{
		Id:      id,
		Config:  &apidevice.Device_Config{Name: name},
		Address: &apidevice.Device_Address{IsReachable: true},
		Details: &apidevice.Device_Light{Light: &apidevice.Light{
			OnOff: &apitrait.OnOff{
				Attributes: &apitrait.OnOff_Attributes{CanControl: true},
				State:      &apitrait.OnOff_State{IsOn: on},
			},
		}},
	}
}

func newTestHouse(t *testing.T, house api.HouseServiceClient, bridge api.BridgeServiceClient) *House {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &House{
		roomID:         "room-1",
		appCtx:         ctx,
		devices:        make(map[string]*ui.Device),
		house:          house,
		bridge:         bridge,
		refreshTimeout: time.Second,
	}
}

// Start must be a no-op once already running (one stream, not one per call),
// and the devices it found via the initial RefreshRoom must be visible
// through Devices().
func TestHouseStartIsIdempotent(t *testing.T) {
	houseClient := &fakeHouseServiceClient{getRoomFn: func(ctx context.Context) (*api.Room, error) {
		return &api.Room{Id: "room-1", Devices: []*apidevice.Device{testLightDevice("light-1", "Lamp", false)}}, nil
	}}
	streamOpened := make(chan struct{}, 1)
	bridgeClient := &fakeBridgeServiceClient{streamFn: func(ctx context.Context) (grpc.ServerStreamingClient[api.Update], error) {
		select {
		case streamOpened <- struct{}{}:
		default:
		}
		return &fakeUpdateStream{ctx: ctx}, nil
	}}

	h := newTestHouse(t, houseClient, bridgeClient)
	defer h.Stop()

	h.Start()
	waitOrFatal(t, streamOpened, "stream was never opened after Start()")
	h.Start()
	h.Start()

	if got := bridgeClient.callCount(); got != 1 {
		t.Fatalf("StreamUpdates called %d times after three Start() calls, want 1", got)
	}

	devices := h.Devices()
	if len(devices) != 1 || devices[0].ID != "light-1" {
		t.Fatalf("Devices() = %+v, want one device light-1", devices)
	}
}

// Stop must block until the streaming goroutine it started has actually
// exited, not just until it has requested cancellation - otherwise a quick
// Stop-then-Start can race a not-yet-unwound previous stream against a new
// one.
func TestHouseStopWaitsForStreamGoroutineToExit(t *testing.T) {
	houseClient := &fakeHouseServiceClient{getRoomFn: func(ctx context.Context) (*api.Room, error) {
		return &api.Room{Id: "room-1"}, nil
	}}

	const recvDelay = 100 * time.Millisecond
	var recvReturned atomic.Bool
	streamOpened := make(chan struct{}, 1)
	bridgeClient := &fakeBridgeServiceClient{streamFn: func(ctx context.Context) (grpc.ServerStreamingClient[api.Update], error) {
		select {
		case streamOpened <- struct{}{}:
		default:
		}
		return &fakeUpdateStream{
			ctx:   ctx,
			delay: recvDelay,
			onReturn: func() {
				recvReturned.Store(true)
			},
		}, nil
	}}

	h := newTestHouse(t, houseClient, bridgeClient)
	h.Start()
	// Make sure stream.Recv() is actually blocked in the fake (and so will
	// observe the cancellation below) before we cancel it, otherwise the
	// run() goroutine could still be mid-scheduling and exit before ever
	// reaching Recv, which would pass this test for the wrong reason.
	waitOrFatal(t, streamOpened, "stream was never opened after Start()")

	h.Stop()

	if !recvReturned.Load() {
		t.Fatalf("Stop() returned before the in-flight stream.Recv() unblocked; Stop no longer waits for run() to exit")
	}
}

// Start's (and run's post-reconnect) RefreshRoom call must be bounded by
// House.refreshTimeout rather than inheriting the stream's long-lived
// context, so a hung bridge can't stall Start() - and with it Show(), which
// runs under the Deck's single global lock - indefinitely.
func TestHouseStartBoundsItsOwnRefreshRoomCall(t *testing.T) {
	getRoomCtxCancelled := make(chan struct{})
	houseClient := &fakeHouseServiceClient{getRoomFn: func(ctx context.Context) (*api.Room, error) {
		<-ctx.Done()
		close(getRoomCtxCancelled)
		return nil, ctx.Err()
	}}
	bridgeClient := &fakeBridgeServiceClient{streamFn: func(ctx context.Context) (grpc.ServerStreamingClient[api.Update], error) {
		return &fakeUpdateStream{ctx: ctx}, nil
	}}

	h := newTestHouse(t, houseClient, bridgeClient)
	h.refreshTimeout = 20 * time.Millisecond
	defer h.Stop()

	started := make(chan struct{})
	go func() {
		h.Start()
		close(started)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("Start() did not return within 2s - its GetRoom call is not bounded by refreshTimeout")
	}

	select {
	case <-getRoomCtxCancelled:
	case <-time.After(time.Second):
		t.Fatalf("GetRoom's context was never cancelled by refreshTimeout")
	}
}

// The update stream's REMOVED action must drop a device from the cache, and
// updates for devices outside our room must be ignored rather than silently
// adopted.
func TestHouseApplyUpdateTracksOnlyRoomDevicesAndHandlesRemoval(t *testing.T) {
	houseClient := &fakeHouseServiceClient{getRoomFn: func(ctx context.Context) (*api.Room, error) {
		return &api.Room{Id: "room-1", Devices: []*apidevice.Device{testLightDevice("light-1", "Lamp", false)}}, nil
	}}

	h := newTestHouse(t, houseClient, nil)
	if err := h.RefreshRoom(context.Background()); err != nil {
		t.Fatalf("RefreshRoom: %s", err)
	}

	// An update for a device that isn't linked to our room is ignored.
	h.applyUpdate(&api.Update{
		Action: api.Update_CHANGED,
		Update: &api.Update_DeviceUpdate{DeviceUpdate: &api.DeviceUpdate{
			Device: testLightDevice("other-room-device", "Other", true),
		}},
	})
	if got := h.Devices(); len(got) != 1 {
		t.Fatalf("Devices() = %+v, want 1 device after an update for an untracked device", got)
	}

	// A CHANGED update for our tracked device updates its state.
	h.applyUpdate(&api.Update{
		Action: api.Update_CHANGED,
		Update: &api.Update_DeviceUpdate{DeviceUpdate: &api.DeviceUpdate{
			Device: testLightDevice("light-1", "Lamp", true),
		}},
	})
	devices := h.Devices()
	if len(devices) != 1 || !devices[0].On {
		t.Fatalf("Devices() = %+v, want light-1 on", devices)
	}

	// A REMOVED update drops it entirely.
	h.applyUpdate(&api.Update{
		Action: api.Update_REMOVED,
		Update: &api.Update_DeviceUpdate{DeviceUpdate: &api.DeviceUpdate{
			DeviceId: "light-1",
		}},
	})
	if got := h.Devices(); len(got) != 0 {
		t.Fatalf("Devices() = %+v, want 0 devices after removal", got)
	}
}

// A device that starts out with no commandable trait (hidden from Devices())
// must still be tracked internally, so that a later stream update granting
// it control is picked up immediately - not just on the next RefreshRoom.
func TestPatchDeviceRecoversControllabilityWithoutRefresh(t *testing.T) {
	readOnly := &apidevice.Device{
		Id: "light-1",
		Details: &apidevice.Device_Light{Light: &apidevice.Light{
			OnOff: &apitrait.OnOff{Attributes: &apitrait.OnOff_Attributes{CanControl: false}},
		}},
	}
	houseClient := &fakeHouseServiceClient{getRoomFn: func(ctx context.Context) (*api.Room, error) {
		return &api.Room{Id: "room-1", Devices: []*apidevice.Device{readOnly}}, nil
	}}

	h := newTestHouse(t, houseClient, nil)
	if err := h.RefreshRoom(context.Background()); err != nil {
		t.Fatalf("RefreshRoom: %s", err)
	}
	if got := h.Devices(); len(got) != 0 {
		t.Fatalf("Devices() = %+v, want 0 devices while read-only", got)
	}

	// Same device, now controllable - delivered as a stream update, not a
	// RefreshRoom. patchDevice must still have it tracked to apply this.
	nowControllable := &apidevice.Device{
		Id: "light-1",
		Details: &apidevice.Device_Light{Light: &apidevice.Light{
			OnOff: &apitrait.OnOff{Attributes: &apitrait.OnOff_Attributes{CanControl: true}},
		}},
	}
	h.applyUpdate(&api.Update{
		Action: api.Update_CHANGED,
		Update: &api.Update_DeviceUpdate{DeviceUpdate: &api.DeviceUpdate{Device: nowControllable}},
	})

	got := h.Devices()
	if len(got) != 1 || got[0].ID != "light-1" {
		t.Fatalf("Devices() = %+v, want light-1 now that it's controllable", got)
	}
}

// rgbToHSB/hsbToRGB should round-trip pure, fully-saturated colours (the
// control screen's own preset palette) cleanly - these are the values
// actually sent/displayed, so small drift here would be visible as a wrong
// swatch highlighted or a slightly-off colour commanded.
func TestRGBHSBRoundTrip(t *testing.T) {
	cases := []struct {
		name          string
		r, g, b       int32
		wantHue       int32
		wantSat, want int32 // saturation, brightness, both percent
	}{
		{"red", 255, 0, 0, 0, 100, 100},
		{"green", 0, 255, 0, 120, 100, 100},
		{"blue", 0, 0, 255, 240, 100, 100},
		{"white", 255, 255, 255, 0, 0, 100},
		{"black", 0, 0, 0, 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hue, sat, bri := rgbToHSB(c.r, c.g, c.b)
			if hue != c.wantHue || sat != c.wantSat || bri != c.want {
				t.Fatalf("rgbToHSB(%d,%d,%d) = (%d,%d,%d), want (%d,%d,%d)",
					c.r, c.g, c.b, hue, sat, bri, c.wantHue, c.wantSat, c.want)
			}
			r, g, b := hsbToRGB(hue, sat, bri)
			if r != c.r || g != c.g || b != c.b {
				t.Fatalf("hsbToRGB(%d,%d,%d) = (%d,%d,%d), want (%d,%d,%d)",
					hue, sat, bri, r, g, b, c.r, c.g, c.b)
			}
		})
	}
}

// Regression test: a device whose Colour trait reports MODE_HSB (e.g. the
// Gledopto zigbee strip) must be commanded with command.Colour_Hsb, not
// command.Colour_Rgb - the house protocol's rgb command value is only valid
// for an RGB-mode device, so sending it to an HSB-mode one is silently
// ignored bridge-side ("pressed red but it isn't going red").
func TestSetColourUsesHSBCommandForHSBModeDevice(t *testing.T) {
	bridgeClient := &fakeBridgeServiceClient{}
	h := newTestHouse(t, nil, bridgeClient)
	h.devices = map[string]*ui.Device{
		"hsb-light": {ID: "hsb-light", ColourUsesHSB: true},
		"rgb-light": {ID: "rgb-light", ColourUsesHSB: false},
	}

	if err := h.SetColour(context.Background(), "hsb-light", 255, 0, 0); err != nil {
		t.Fatalf("SetColour: %s", err)
	}
	hsb := bridgeClient.getLastCommand().GetColour().GetHsb()
	if hsb == nil {
		t.Fatalf("command sent for HSB-mode device = %+v, want a Colour_Hsb value", bridgeClient.getLastCommand())
	}
	if hsb.GetHue() != 0 || hsb.GetSaturation() != 100 || hsb.GetBrightness() != 100 {
		t.Fatalf("hsb = %+v, want red (hue=0, sat=100, brightness=100)", hsb)
	}

	if err := h.SetColour(context.Background(), "rgb-light", 255, 0, 0); err != nil {
		t.Fatalf("SetColour: %s", err)
	}
	rgb := bridgeClient.getLastCommand().GetColour().GetRgb()
	if rgb == nil {
		t.Fatalf("command sent for RGB-mode device = %+v, want a Colour_Rgb value", bridgeClient.getLastCommand())
	}
	if rgb.GetRed() != 255 || rgb.GetGreen() != 0 || rgb.GetBlue() != 0 {
		t.Fatalf("rgb = %+v, want (255,0,0)", rgb)
	}
}

// Only a bare Generic on/off device should be badged as an Outlet; other
// named device kinds with on/off control (e.g. a television) shouldn't be
// mislabeled with a plug icon just because they lack a more specific Kind.
func TestToViewDeviceOnlyBadgesGenericOnOffDevicesAsOutlet(t *testing.T) {
	generic := &apidevice.Device{
		Id: "plug-1",
		Details: &apidevice.Device_Generic{Generic: &apidevice.Generic{
			OnOff: &apitrait.OnOff{
				Attributes: &apitrait.OnOff_Attributes{CanControl: true},
				State:      &apitrait.OnOff_State{IsOn: true},
			},
		}},
	}
	if got := toViewDevice(generic); got.Kind != ui.DeviceKindOutlet {
		t.Fatalf("generic on/off device Kind = %v, want DeviceKindOutlet", got.Kind)
	}

	television := &apidevice.Device{
		Id: "tv-1",
		Details: &apidevice.Device_Television{Television: &apidevice.Television{
			OnOff: &apitrait.OnOff{
				Attributes: &apitrait.OnOff_Attributes{CanControl: true},
				State:      &apitrait.OnOff_State{IsOn: true},
			},
		}},
	}
	if got := toViewDevice(television); got.Kind == ui.DeviceKindOutlet {
		t.Fatalf("television Kind = %v, want anything but DeviceKindOutlet", got.Kind)
	}
	if !toViewDevice(television).CanToggle {
		t.Fatalf("television CanToggle = false, want true")
	}
}

// A standing desk's Position trait (unit-based, no SupportsSet) can't be
// commanded directly - only its named Mode presets can. StandMode/SitMode
// should be picked out by substring match against whatever names the
// bridge actually reports, not a hardcoded exact string.
func TestToViewDeviceParsesStandingDeskPresets(t *testing.T) {
	desk := &apidevice.Device{
		Id: "desk-1",
		Details: &apidevice.Device_StandingDesk{StandingDesk: &apidevice.StandingDesk{
			Mode: &apitrait.Mode{
				Attributes: &apitrait.Mode_Attributes{
					CanControl:     true,
					AvailableModes: []string{"preset_1", "preset_2", "preset_3_stand", "preset_4_sit"},
				},
			},
		}},
	}
	got := toViewDevice(desk)
	if got.Kind != ui.DeviceKindStandingDesk {
		t.Fatalf("Kind = %v, want DeviceKindStandingDesk", got.Kind)
	}
	if got.StandMode != "preset_3_stand" {
		t.Fatalf("StandMode = %q, want %q", got.StandMode, "preset_3_stand")
	}
	if got.SitMode != "preset_4_sit" {
		t.Fatalf("SitMode = %q, want %q", got.SitMode, "preset_4_sit")
	}
	if !got.IsControllable() {
		t.Fatal("IsControllable() = false, want true")
	}
}

// Devices with no commandable trait at all (e.g. a motion sensor) should be
// dropped from RefreshRoom's result rather than listed with dead buttons.
func TestRefreshRoomDropsUncontrollableDevices(t *testing.T) {
	sensor := &apidevice.Device{
		Id:      "sensor-1",
		Details: &apidevice.Device_Sensor{Sensor: &apidevice.Sensor{}},
	}
	light := &apidevice.Device{
		Id: "light-1",
		Details: &apidevice.Device_Light{Light: &apidevice.Light{
			OnOff: &apitrait.OnOff{Attributes: &apitrait.OnOff_Attributes{CanControl: true}},
		}},
	}

	h := &House{
		house: &fakeHouseServiceClient{
			getRoomFn: func(ctx context.Context) (*api.Room, error) {
				return &api.Room{Devices: []*apidevice.Device{sensor, light}}, nil
			},
		},
		devices: make(map[string]*ui.Device),
	}
	if err := h.RefreshRoom(context.Background()); err != nil {
		t.Fatalf("RefreshRoom() error = %v", err)
	}

	got := h.Devices()
	if len(got) != 1 || got[0].ID != "light-1" {
		t.Fatalf("Devices() = %+v, want only light-1", got)
	}
}
