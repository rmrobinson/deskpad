package controllers

import (
	"context"
	"log"
	"strconv"
	"sync"
	"time"

	api "github.com/rmrobinson/house/api"
	"github.com/rmrobinson/house/api/command"
	apidevice "github.com/rmrobinson/house/api/device"
	apitrait "github.com/rmrobinson/house/api/trait"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
)

// bridgePollInterval bounds how long a state change made outside of
// ExecuteCommand - a key press that changes screen, playback paused from
// another client entirely - can take to reach StreamUpdates subscribers.
const bridgePollInterval = 2 * time.Second

// bridgeAsyncCommandTimeout bounds how long ExecuteCommandAsync's background
// goroutine will wait on the underlying integration call, so a blocked call
// can't leak the goroutine forever.
const bridgeAsyncCommandTimeout = 5 * time.Second

// BridgeMediaPlayerController is the subset of local media playback and
// volume control the Bridge exposes as the house Media and Volume traits.
// SpotifyMediaPlayer and LinuxMediaPlayer both already satisfy it.
type BridgeMediaPlayerController interface {
	Play()
	Pause()
	FastForward()
	Rewind()
	Mute()
	Unmute()
	IsPlaying() bool
	IsMuted() bool
	GetVolume() int
	SetVolume(pct int)
	CurrentlyPlaying() *ui.MediaItem
}

// Bridge implements the house BridgeService, exposing this DeskPad instance
// as a single MediaPlayer device: Media (local playback), Volume (local
// output level), and App (which screen is on-deck). This is the inverse of
// the House controller, which lets DeskPad control other bridges - the two
// are deliberately separate client/server roles for now; unifying DeskPad
// into one fully decoupled model is a larger, unrelated effort.
type Bridge struct {
	api.UnimplementedBridgeServiceServer

	id   string
	deck *deskpad.Deck
	// apps lists the screens selectable through the App trait, in order.
	// apps[0] is treated as the default/home screen that an AppClose command
	// returns to.
	apps  []deskpad.Screen
	media BridgeMediaPlayerController

	mu      sync.Mutex
	name    string
	version uint64

	subMu sync.Mutex
	subs  map[chan *api.Update]struct{}

	pollMu     sync.Mutex
	pollCancel context.CancelFunc
	pollDone   chan struct{}
}

// NewBridge creates a Bridge exposing deck/media as a single device with the
// given stable id and display name. apps lists the screens selectable
// through the App trait, in order; apps[0] is treated as the default screen.
func NewBridge(id, name string, deck *deskpad.Deck, apps []deskpad.Screen, media BridgeMediaPlayerController) *Bridge {
	return &Bridge{
		id:    id,
		name:  name,
		deck:  deck,
		apps:  apps,
		media: media,
		subs:  make(map[chan *api.Update]struct{}),
	}
}

var _ api.BridgeServiceServer = (*Bridge)(nil)

// GetBridge returns this DeskPad instance's own bridge-level identity.
func (b *Bridge) GetBridge(ctx context.Context, req *api.GetBridgeRequest) (*api.Bridge, error) {
	return &api.Bridge{
		Id:          b.id,
		IsReachable: true,
		Config:      &api.Bridge_Config{Name: b.deviceName()},
		State:       &api.Bridge_State{IsPaired: true},
	}, nil
}

// ListDevices always returns the single device this Bridge represents.
func (b *Bridge) ListDevices(ctx context.Context, req *api.ListDevicesRequest) (*api.ListDevicesResponse, error) {
	return &api.ListDevicesResponse{Devices: []*apidevice.Device{b.device()}}, nil
}

// GetDevice returns this Bridge's device if id matches, else NotFound.
func (b *Bridge) GetDevice(ctx context.Context, req *api.GetDeviceRequest) (*apidevice.Device, error) {
	if req.GetId() != b.id {
		return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetId())
	}
	return b.device(), nil
}

// UpdateDeviceConfig currently only supports renaming the device.
func (b *Bridge) UpdateDeviceConfig(ctx context.Context, req *api.UpdateDeviceConfigRequest) (*apidevice.Device, error) {
	if req.GetId() != b.id {
		return nil, status.Errorf(codes.NotFound, "device %q not found", req.GetId())
	}
	if v := req.GetVersion(); v != "" && v != b.currentVersion() {
		return nil, status.Errorf(codes.FailedPrecondition, "device %q version mismatch", b.id)
	}

	if name := req.GetConfig().GetName(); name != "" {
		b.mu.Lock()
		b.name = name
		b.mu.Unlock()
	}

	b.bumpVersion()
	d := b.device()
	b.broadcastDeviceUpdate(api.Update_CHANGED, d)
	return d, nil
}

// ExecuteCommand applies cmd synchronously and returns the resulting device
// state.
func (b *Bridge) ExecuteCommand(ctx context.Context, cmd *command.Command) (*apidevice.Device, error) {
	if cmd.GetDeviceId() != b.id {
		return nil, status.Errorf(codes.NotFound, "device %q not found", cmd.GetDeviceId())
	}
	if v := cmd.GetVersion(); v != "" && v != b.currentVersion() {
		return nil, status.Errorf(codes.FailedPrecondition, "device %q version mismatch", b.id)
	}

	if err := b.apply(ctx, cmd); err != nil {
		return nil, err
	}

	b.bumpVersion()
	d := b.device()
	b.broadcastDeviceUpdate(api.Update_CHANGED, d)
	return d, nil
}

// ExecuteCommandAsync validates cmd, then applies it in the background and
// reports the outcome on StreamUpdates as a CommandUpdate keyed by cmd.Id.
func (b *Bridge) ExecuteCommandAsync(ctx context.Context, cmd *command.Command) (*api.ExecuteCommandAsyncResponse, error) {
	if cmd.GetDeviceId() != b.id {
		return nil, status.Errorf(codes.NotFound, "device %q not found", cmd.GetDeviceId())
	}
	if v := cmd.GetVersion(); v != "" && v != b.currentVersion() {
		return nil, status.Errorf(codes.FailedPrecondition, "device %q version mismatch", b.id)
	}

	go func() {
		// Detached from ctx: the caller isn't waiting on this call to learn
		// the outcome (that's the point of "async"), but the goroutine still
		// needs its own bound so a blocked integration call can't leak it
		// forever.
		applyCtx, cancel := context.WithTimeout(context.Background(), bridgeAsyncCommandTimeout)
		defer cancel()

		var resultProto *rpcstatus.Status
		var resultingVersion string
		if err := b.apply(applyCtx, cmd); err != nil {
			resultProto = status.Convert(err).Proto()
		} else {
			resultingVersion = b.bumpVersion()
			b.broadcastDeviceUpdate(api.Update_CHANGED, b.device())
			resultProto = status.New(codes.OK, "").Proto()
		}
		b.broadcastCommandUpdate(cmd.GetId(), resultingVersion, resultProto)
	}()

	return &api.ExecuteCommandAsyncResponse{}, nil
}

// StreamUpdates sends an initial snapshot, then forwards every subsequent
// Update until the caller disconnects. Polling for externally-driven state
// changes only runs while at least one subscriber is attached.
func (b *Bridge) StreamUpdates(req *api.StreamUpdatesRequest, stream api.BridgeService_StreamUpdatesServer) error {
	ch := make(chan *api.Update, 16)

	b.subMu.Lock()
	b.subs[ch] = struct{}{}
	first := len(b.subs) == 1
	b.subMu.Unlock()
	if first {
		b.startPolling()
	}
	defer func() {
		b.subMu.Lock()
		delete(b.subs, ch)
		last := len(b.subs) == 0
		b.subMu.Unlock()
		if last {
			b.stopPolling()
		}
	}()

	initial := &api.Update{
		Action: api.Update_INITIAL,
		Update: &api.Update_InitialUpdate{InitialUpdate: &api.InitialUpdate{
			Bridge:  &api.Bridge{Id: b.id, Config: &api.Bridge_Config{Name: b.deviceName()}},
			Devices: []*apidevice.Device{b.device()},
		}},
	}
	if err := stream.Send(initial); err != nil {
		return err
	}

	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return nil
		case u := <-ch:
			if err := stream.Send(u); err != nil {
				return err
			}
		}
	}
}

// apply dispatches a single command to the backing media controller or the
// deck's screen switch. The command's device_id and version are assumed
// already validated by the caller.
func (b *Bridge) apply(ctx context.Context, cmd *command.Command) error {
	switch d := cmd.GetDetails().(type) {
	case *command.Command_Playback:
		switch d.Playback.GetAction() {
		case command.Playback_ACTION_PLAY:
			b.media.Play()
		case command.Playback_ACTION_PAUSE, command.Playback_ACTION_STOP:
			b.media.Pause()
		default:
			return status.Errorf(codes.InvalidArgument, "unsupported playback action %v", d.Playback.GetAction())
		}
	case *command.Command_SeekRelative:
		// The local players only expose fixed-step fast-forward/rewind, not
		// an arbitrary seek, so delta_s's magnitude is ignored - only its
		// sign is honoured.
		if d.SeekRelative.GetDeltaS() < 0 {
			b.media.Rewind()
		} else {
			b.media.FastForward()
		}
	case *command.Command_SeekAbsolute:
		return status.Error(codes.Unimplemented, "absolute seek is not supported")
	case *command.Command_VolumeAbsolute:
		b.media.SetVolume(int(d.VolumeAbsolute.GetLevel()))
	case *command.Command_VolumeRelative:
		b.media.SetVolume(b.media.GetVolume() + int(d.VolumeRelative.GetDelta()))
	case *command.Command_Mute:
		if d.Mute.GetIsMuted() {
			b.media.Mute()
		} else {
			b.media.Unmute()
		}
	case *command.Command_AppLaunch:
		return b.launchApp(ctx, d.AppLaunch.GetApplicationId())
	case *command.Command_AppClose:
		return b.launchApp(ctx, b.defaultAppID())
	default:
		return status.Error(codes.InvalidArgument, "command not supported by this device")
	}
	return nil
}

func (b *Bridge) launchApp(ctx context.Context, id string) error {
	for _, s := range b.apps {
		if s.Name() == id {
			b.deck.ChangeScreen(ctx, s)
			return nil
		}
	}
	return status.Errorf(codes.NotFound, "app %q not found", id)
}

func (b *Bridge) defaultAppID() string {
	if len(b.apps) == 0 {
		return ""
	}
	return b.apps[0].Name()
}

func (b *Bridge) deviceName() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.name
}

func (b *Bridge) currentVersion() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strconv.FormatUint(b.version, 10)
}

func (b *Bridge) bumpVersion() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.version++
	return strconv.FormatUint(b.version, 10)
}

// device builds a fresh device snapshot by reading the backing controllers
// live - like a screen's Show(), it owns no durable state of its own beyond
// name/version.
func (b *Bridge) device() *apidevice.Device {
	return &apidevice.Device{
		Id:      b.id,
		Config:  &apidevice.Device_Config{Name: b.deviceName()},
		Address: &apidevice.Device_Address{IsReachable: true},
		Version: b.currentVersion(),
		Details: &apidevice.Device_MediaPlayer{MediaPlayer: &apidevice.MediaPlayer{
			Volume: b.volumeTrait(),
			App:    b.appTrait(),
			Media:  b.mediaTrait(),
		}},
	}
}

func (b *Bridge) volumeTrait() *apitrait.Volume {
	return &apitrait.Volume{
		Attributes: &apitrait.Volume_Attributes{
			CanControl:   true,
			CanMute:      true,
			MaximumLevel: 100,
		},
		State: &apitrait.Volume_State{
			IsMuted: b.media.IsMuted(),
			Level:   int32(b.media.GetVolume()),
		},
	}
}

func (b *Bridge) mediaTrait() *apitrait.Media {
	item := b.media.CurrentlyPlaying()

	state := &apitrait.Media_State{
		DeviceState:   apitrait.Media_DEVICE_STATE_INACTIVE,
		PlaybackState: apitrait.Media_PS_PAUSED,
	}
	if b.media.IsPlaying() {
		state.PlaybackState = apitrait.Media_PS_PLAYING
	}
	if item != nil {
		state.DeviceState = apitrait.Media_DEVICE_STATE_ACTIVE
		state.MediaType = apitrait.Media_TYPE_SONG
		songDetails := &apitrait.Media_SongDetails{
			Id:        item.ID,
			SongName:  item.Title,
			Artists:   item.Artists,
			AlbumName: item.AlbumName,
		}
		if item.AlburmArtURL != "" {
			url := item.AlburmArtURL
			songDetails.AlbumArtUrl = &url
		}
		state.SongDetails = songDetails
	}

	canTrue := true
	// Not exposed: the local players only support fixed-step fast-forward
	// and rewind (surfaced above as CanSeek), not an explicit skip-track
	// command - there's no Command variant for it to attach to yet.
	canFalse := false
	return &apitrait.Media{
		Attributes: &apitrait.Media_Attributes{
			CanControl:      true,
			CanPause:        &canTrue,
			CanSeek:         &canTrue,
			CanSkipForward:  &canFalse,
			CanSkipBackward: &canFalse,
		},
		State: state,
	}
}

func (b *Bridge) appTrait() *apitrait.App {
	apps := make([]*apitrait.App_Instance, len(b.apps))
	for i, s := range b.apps {
		apps[i] = &apitrait.App_Instance{Id: s.Name(), Name: s.Name()}
	}

	current := ""
	if s := b.deck.Screen(); s != nil {
		current = s.Name()
	}

	return &apitrait.App{
		Attributes: &apitrait.App_Attributes{
			CanControl:   true,
			Applications: apps,
		},
		State: &apitrait.App_State{ApplicationId: current},
	}
}

// bridgeSnapshot is the subset of device state the poll loop diffs on each
// tick to detect changes ExecuteCommand didn't cause - e.g. a physical key
// press that changed screen, or playback paused from another client.
type bridgeSnapshot struct {
	playing bool
	muted   bool
	volume  int
	appID   string
	itemID  string
}

func (b *Bridge) snapshot() bridgeSnapshot {
	itemID := ""
	if item := b.media.CurrentlyPlaying(); item != nil {
		itemID = item.ID
	}
	appID := ""
	if s := b.deck.Screen(); s != nil {
		appID = s.Name()
	}

	return bridgeSnapshot{
		playing: b.media.IsPlaying(),
		muted:   b.media.IsMuted(),
		volume:  b.media.GetVolume(),
		appID:   appID,
		itemID:  itemID,
	}
}

// startPolling is a no-op if the poll loop is already running. Call it when
// a subscriber attaches.
func (b *Bridge) startPolling() {
	b.pollMu.Lock()
	defer b.pollMu.Unlock()
	if b.pollCancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	b.pollCancel = cancel
	done := make(chan struct{})
	b.pollDone = done

	go func() {
		defer close(done)
		last := b.snapshot()
		ticker := time.NewTicker(bridgePollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				cur := b.snapshot()
				if cur == last {
					continue
				}
				last = cur
				b.bumpVersion()
				b.broadcastDeviceUpdate(api.Update_CHANGED, b.device())
			}
		}
	}()
}

// stopPolling tears down the poll loop and waits for it to exit, so DeskPad
// isn't left polling against its own controllers while nobody is
// subscribed. Call it when the last subscriber detaches.
func (b *Bridge) stopPolling() {
	b.pollMu.Lock()
	cancel := b.pollCancel
	done := b.pollDone
	b.pollCancel = nil
	b.pollDone = nil
	b.pollMu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
}

func (b *Bridge) broadcastDeviceUpdate(action api.Update_Action, d *apidevice.Device) {
	b.publish(&api.Update{
		Action: action,
		Update: &api.Update_DeviceUpdate{DeviceUpdate: &api.DeviceUpdate{
			Device:   d,
			DeviceId: d.GetId(),
			BridgeId: b.id,
		}},
	})
}

func (b *Bridge) broadcastCommandUpdate(commandID, resultingVersion string, result *rpcstatus.Status) {
	if commandID == "" {
		return
	}
	b.publish(&api.Update{
		Action: api.Update_EXECUTED,
		Update: &api.Update_CommandUpdate{CommandUpdate: &api.CommandUpdate{
			CommandId:        commandID,
			BridgeId:         b.id,
			DeviceId:         b.id,
			Result:           result,
			ResultingVersion: resultingVersion,
		}},
	})
}

func (b *Bridge) publish(u *api.Update) {
	b.subMu.Lock()
	defer b.subMu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- u:
		default:
			log.Printf("[bridge] dropping update for a slow StreamUpdates subscriber\n")
		}
	}
}
