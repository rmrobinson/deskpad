package controllers

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	api "github.com/rmrobinson/house/api"
	"github.com/rmrobinson/house/api/command"
	apidevice "github.com/rmrobinson/house/api/device"
	apitrait "github.com/rmrobinson/house/api/trait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"

	"github.com/rmrobinson/deskpad/ui"
)

const houseMaxBackoff = 30 * time.Second

// houseRefreshTimeout bounds the GetRoom call Start/run make on their own
// initiative (as opposed to the one a screen's manual refresh key makes,
// which is already bounded by the Deck's own per-keypress timeout). Without
// a bound here, a slow or unreachable house bridge would stall Start() -
// and with it Show(), and with it every key on the deck, since Show() runs
// while the Deck holds its single global press lock - for as long as the
// underlying dial/keepalive takes to notice, rather than a fixed, short time.
const houseRefreshTimeout = 5 * time.Second

// House maintains the set of devices linked to one configured room, kept
// fresh by a live BridgeService.StreamUpdates subscription while active -
// unlike most other DeskPad integrations, house device state can change from
// outside DeskPad entirely (a wall switch, adminui, another client), so it
// isn't enough to only refresh on button press.
//
// The subscription is only held open while a screen backed by this
// controller is actually on screen: Start/Stop are called by those screens
// as the user navigates in and out, so DeskPad isn't holding an idle
// subscription against the house bridge the rest of the time.
type House struct {
	addr                      string
	useTLS                    bool
	caCert, certFile, keyFile string
	serverName                string
	roomID                    string

	appCtx context.Context

	conn   *grpc.ClientConn
	house  api.HouseServiceClient
	bridge api.BridgeServiceClient

	mu      sync.RWMutex
	devices map[string]*ui.Device
	order   []string

	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}

	// refreshTimeout bounds Start/run's own-initiative RefreshRoom calls;
	// defaults to houseRefreshTimeout, overridable by tests.
	refreshTimeout time.Duration
}

// NewHouse dials addr and returns a House controller scoped to roomID.
// appCtx bounds the lifetime of any streaming connection Start later opens;
// it's typically the application's top-level context, not a per-request one.
// Set useTLS to enable TLS; caCert overrides the system roots (leave empty to
// use them); certFile/keyFile, if both set, present a client certificate for
// mutual TLS. All three are re-read from disk on every handshake rather than
// cached here, so a cert-rotation sidecar can rotate them without restarting
// DeskPad. serverName overrides the hostname the server's certificate is
// verified against; leave empty when addr is already that name. It's
// required whenever addr isn't - e.g. house's own step-ca-issued bridge
// certs are for "<name>.<host>.house.internal", essentially never the
// literal address a client actually dials.
func NewHouse(appCtx context.Context, addr string, useTLS bool, caCert, certFile, keyFile, serverName, roomID string) (*House, error) {
	h := &House{
		addr:           addr,
		useTLS:         useTLS,
		caCert:         caCert,
		certFile:       certFile,
		keyFile:        keyFile,
		serverName:     serverName,
		roomID:         roomID,
		appCtx:         appCtx,
		devices:        make(map[string]*ui.Device),
		refreshTimeout: houseRefreshTimeout,
	}

	opts, err := h.dialOpts()
	if err != nil {
		return nil, err
	}
	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", addr, err)
	}
	h.conn = conn
	h.house = api.NewHouseServiceClient(conn)
	h.bridge = api.NewBridgeServiceClient(conn)
	return h, nil
}

func (h *House) dialOpts() ([]grpc.DialOption, error) {
	var credOpt grpc.DialOption
	if !h.useTLS {
		credOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	} else {
		tlsCfg := &tls.Config{ServerName: h.serverName}
		if h.caCert != "" {
			pem, err := os.ReadFile(h.caCert)
			if err != nil {
				return nil, fmt.Errorf("read ca-cert %s: %w", h.caCert, err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, fmt.Errorf("ca-cert %s: no valid PEM certificates found", h.caCert)
			}
			tlsCfg.RootCAs = pool
		}
		if h.certFile != "" && h.keyFile != "" {
			certFile, keyFile := h.certFile, h.keyFile
			tlsCfg.GetClientCertificate = func(*tls.CertificateRequestInfo) (*tls.Certificate, error) {
				cert, err := tls.LoadX509KeyPair(certFile, keyFile)
				if err != nil {
					return nil, fmt.Errorf("load client cert/key: %w", err)
				}
				return &cert, nil
			}
		}
		credOpt = grpc.WithTransportCredentials(credentials.NewTLS(tlsCfg))
	}
	kaOpt := grpc.WithKeepaliveParams(keepalive.ClientParameters{
		Time:                30 * time.Second,
		Timeout:             10 * time.Second,
		PermitWithoutStream: true,
	})
	return []grpc.DialOption{credOpt, kaOpt}, nil
}

// Start ensures the device-update stream is running; it's a no-op if already
// started. Safe to call on every Show() of a screen backed by this
// controller - it only actually does work the first time.
func (h *House) Start() {
	h.runMu.Lock()
	defer h.runMu.Unlock()
	if h.cancel != nil {
		return
	}

	ctx, cancel := context.WithCancel(h.appCtx)
	h.cancel = cancel
	done := make(chan struct{})
	h.done = done

	h.refreshRoomBounded(ctx)

	go func() {
		defer close(done)
		h.run(ctx)
	}()
}

// Stop tears down the device-update stream and waits for the streaming
// goroutine to actually exit before returning, so a caller that immediately
// calls Start() again (e.g. a quick navigate-away-and-back) can't end up
// racing a not-yet-unwound previous run against the new one. Call it when
// navigating away from every screen backed by this controller, so DeskPad
// isn't left holding an open subscription against the house bridge while
// nobody's looking at it.
func (h *House) Stop() {
	h.runMu.Lock()
	cancel := h.cancel
	done := h.done
	h.cancel = nil
	h.done = nil
	h.runMu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	if done != nil {
		<-done
	}
}

// refreshRoomBounded calls RefreshRoom with h.refreshTimeout applied on top
// of ctx, so a slow/unreachable bridge can't stall the caller (Start, or
// run's post-reconnect refresh) indefinitely.
func (h *House) refreshRoomBounded(ctx context.Context) {
	refreshCtx, cancel := context.WithTimeout(ctx, h.refreshTimeout)
	defer cancel()
	if err := h.RefreshRoom(refreshCtx); err != nil {
		log.Printf("[house] refresh room %s: %v\n", h.roomID, err)
	}
}

// RefreshRoom re-resolves which devices are linked to the configured room and
// seeds their current state, replacing the previous cache wholesale.
func (h *House) RefreshRoom(ctx context.Context) error {
	room, err := h.house.GetRoom(ctx, &api.GetRoomRequest{Id: h.roomID})
	if err != nil {
		return fmt.Errorf("get room %s: %w", h.roomID, err)
	}

	order := make([]string, 0, len(room.GetDevices()))
	devices := make(map[string]*ui.Device, len(room.GetDevices()))
	for _, d := range room.GetDevices() {
		devices[d.GetId()] = toViewDevice(d)
		order = append(order, d.GetId())
	}

	h.mu.Lock()
	h.devices = devices
	h.order = order
	h.mu.Unlock()
	return nil
}

// Devices returns a snapshot of the room's controllable devices, in the
// order GetRoom reported them. Membership (h.devices/h.order) tracks every
// device linked to the room regardless of controllability - filtering
// happens only here, at read time, rather than by dropping uncontrollable
// devices from the tracked set. That matters because patchDevice only
// applies an update to an id it's already tracking: if an uncontrollable
// device were removed from membership instead, a later update that made it
// controllable again would never be applied (patchDevice would just keep
// rejecting it as untracked) until the next full RefreshRoom.
func (h *House) Devices() []ui.Device {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]ui.Device, 0, len(h.order))
	for _, id := range h.order {
		if d, ok := h.devices[id]; ok && d.IsControllable() {
			out = append(out, *d)
		}
	}
	return out
}

// SetOnOff turns deviceID on or off.
func (h *House) SetOnOff(ctx context.Context, deviceID string, on bool) error {
	d, err := h.bridge.ExecuteCommand(ctx, &command.Command{
		DeviceId: deviceID,
		Details:  &command.Command_OnOff{OnOff: &command.OnOff{On: on}},
	})
	if err != nil {
		return err
	}
	h.patchDevice(d)
	return nil
}

// SetBrightness sets deviceID's brightness to pct (0-100, clamped).
func (h *House) SetBrightness(ctx context.Context, deviceID string, pct int32) error {
	if pct < 0 {
		pct = 0
	} else if pct > 100 {
		pct = 100
	}
	d, err := h.bridge.ExecuteCommand(ctx, &command.Command{
		DeviceId: deviceID,
		Details:  &command.Command_BrightnessAbsolute{BrightnessAbsolute: &command.BrightnessAbsolute{BrightnessPercent: pct}},
	})
	if err != nil {
		return err
	}
	h.patchDevice(d)
	return nil
}

// SetColour sets deviceID's colour to the given RGB (0-255 per channel).
func (h *House) SetColour(ctx context.Context, deviceID string, r, g, b int32) error {
	colour := &command.Colour{Value: &command.Colour_Rgb{Rgb: &command.Colour_RGB{Red: r, Green: g, Blue: b}}}

	// The house protocol's rgb command value is only valid for an RGB-mode
	// device (api/command/colour.proto's own comment on it) - an HSB-mode
	// device like the Gledopto zigbee strip just ignores it, so the preset
	// swatch silently does nothing. Convert to HSB for those instead.
	h.mu.RLock()
	usesHSB := h.devices[deviceID] != nil && h.devices[deviceID].ColourUsesHSB
	h.mu.RUnlock()
	if usesHSB {
		hue, saturation, brightness := rgbToHSB(r, g, b)
		colour = &command.Colour{Value: &command.Colour_Hsb{Hsb: &command.Colour_HSB{
			Hue: hue, Saturation: saturation, Brightness: brightness,
		}}}
	}

	d, err := h.bridge.ExecuteCommand(ctx, &command.Command{
		DeviceId: deviceID,
		Details:  &command.Command_Colour{Colour: colour},
	})
	if err != nil {
		return err
	}
	h.patchDevice(d)
	return nil
}

// SetMode sends deviceID's Mode trait to mode (e.g. a standing desk preset
// like "preset_3_stand").
func (h *House) SetMode(ctx context.Context, deviceID, mode string) error {
	d, err := h.bridge.ExecuteCommand(ctx, &command.Command{
		DeviceId: deviceID,
		Details:  &command.Command_Mode{Mode: &command.Mode{Value: mode}},
	})
	if err != nil {
		return err
	}
	h.patchDevice(d)
	return nil
}

// SetSpeed sets deviceID's speed to value, in the device's own units.
func (h *House) SetSpeed(ctx context.Context, deviceID string, value int32) error {
	d, err := h.bridge.ExecuteCommand(ctx, &command.Command{
		DeviceId: deviceID,
		Details:  &command.Command_Speed{Speed: &command.Speed{Value: value}},
	})
	if err != nil {
		return err
	}
	h.patchDevice(d)
	return nil
}

// run streams device updates until ctx is cancelled (by Stop, or by appCtx
// ending), reconnecting with exponential backoff on any error.
func (h *House) run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		wasConnected, err := h.runStream(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil {
			log.Printf("[house] stream: %v; reconnect in %s\n", err, backoff)
		}
		if wasConnected {
			backoff = time.Second
		} else if backoff < houseMaxBackoff {
			backoff *= 2
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}

		// Membership (which devices are linked to our room) can only change
		// while we're not actively watching for it, so re-resolve it on every
		// reconnect rather than just once at Start.
		h.refreshRoomBounded(ctx)
	}
}

func (h *House) runStream(ctx context.Context) (wasConnected bool, _ error) {
	stream, err := h.bridge.StreamUpdates(ctx, &api.StreamUpdatesRequest{})
	if err != nil {
		if ctx.Err() != nil {
			return false, nil
		}
		return false, fmt.Errorf("open stream: %w", err)
	}

	log.Printf("[house] stream connected to %s\n", h.addr)
	for {
		update, err := stream.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return wasConnected, nil
			}
			return wasConnected, fmt.Errorf("recv: %w", err)
		}
		wasConnected = true
		h.applyUpdate(update)
	}
}

func (h *House) applyUpdate(update *api.Update) {
	switch u := update.GetUpdate().(type) {
	case *api.Update_InitialUpdate:
		for _, d := range u.InitialUpdate.GetDevices() {
			h.patchDevice(d)
		}
	case *api.Update_DeviceUpdate:
		if update.GetAction() == api.Update_REMOVED {
			h.removeDevice(u.DeviceUpdate.GetDeviceId())
			return
		}
		h.patchDevice(u.DeviceUpdate.GetDevice())
	}
}

// patchDevice updates the cached entry for d if d is linked to our room,
// ignoring state for every other device on the bridge mesh.
func (h *House) patchDevice(d *apidevice.Device) {
	if d == nil || d.GetId() == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, tracked := h.devices[d.GetId()]; !tracked {
		return
	}
	// Store the update regardless of IsControllable() - membership tracking
	// stays independent of it (see Devices()), so a device that regains a
	// control later is picked straight back up instead of needing a
	// RefreshRoom to re-establish membership.
	h.devices[d.GetId()] = toViewDevice(d)
}

func (h *House) removeDevice(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.devices, id)
}

// toViewDevice converts a house API device into the plain view-model screens
// consume. The OnOff/Brightness/Colour/Speed traits each sit at a different
// spot depending on which device kind this is - mirrors the type-switch shape
// house's own service/lib/houseview package uses for the same purpose.
func toViewDevice(d *apidevice.Device) *ui.Device {
	v := &ui.Device{
		ID:     d.GetId(),
		Name:   displayName(d),
		Online: d.GetAddress().GetIsReachable(),
	}

	switch {
	case d.GetLight() != nil:
		v.Kind = ui.DeviceKindLight
	case d.GetFan() != nil:
		v.Kind = ui.DeviceKindFan
	case d.GetStandingDesk() != nil:
		v.Kind = ui.DeviceKindStandingDesk
	default:
		v.Kind = ui.DeviceKindOther
	}

	if onOff := deviceOnOff(d); onOff != nil && onOff.GetAttributes().GetCanControl() {
		// Only a bare Generic device (no more specific kind of its own, such
		// as a television or thermostat) gets badged as an outlet - those
		// are the on/off-only devices actually shaped like a smart plug.
		// Promoting every other on/off-capable kind to Outlet would mislabel
		// known device types with a plug icon they don't match.
		if v.Kind == ui.DeviceKindOther && d.GetGeneric() != nil {
			v.Kind = ui.DeviceKindOutlet
		}
		v.CanToggle = true
		v.On = onOff.GetState().GetIsOn()
	}
	if b := deviceBrightness(d); b != nil && b.GetAttributes().GetCanControl() {
		v.CanDim = true
		v.BrightnessPct = int(b.GetState().GetLevel())
	}
	if c := deviceColour(d); c != nil && c.GetAttributes().GetCanControl() {
		v.CanColour = true
		v.ColourUsesHSB = c.GetAttributes().GetMode() == apitrait.Colour_Attributes_MODE_HSB
		if v.ColourUsesHSB {
			hsb := c.GetState().GetHsb()
			v.ColourR, v.ColourG, v.ColourB = hsbToRGB(hsb.GetHue(), hsb.GetSaturation(), hsb.GetBrightness())
		} else {
			rgb := c.GetState().GetRgb()
			v.ColourR, v.ColourG, v.ColourB = rgb.GetRed(), rgb.GetGreen(), rgb.GetBlue()
		}
	}
	if s := deviceSpeed(d); s != nil && s.GetAttributes().GetCanControl() {
		v.CanAdjustSpeed = true
		v.Speed = s.GetState().GetCurrentSpeed()
		v.MinSpeed = s.GetAttributes().GetMinimumSpeed()
		v.MaxSpeed = s.GetAttributes().GetMaximumSpeed()
		v.SpeedIncrement = s.GetAttributes().GetSpeedIncrement()
	}
	if m := deviceMode(d); m != nil && m.GetAttributes().GetCanControl() {
		// A standing desk's Position trait here is unit-based (cm) without
		// SupportsSet (no direct jump-to-value/percent) - the only way to
		// move it is the named presets its Mode trait offers, e.g.
		// "preset_3_stand"/"preset_4_sit". Match by substring rather than
		// hardcoding those exact names so any bridge's own preset naming
		// works the same way.
		for _, mode := range m.GetAttributes().GetAvailableModes() {
			switch lower := strings.ToLower(mode); {
			case strings.Contains(lower, "stand"):
				v.StandMode = mode
			case strings.Contains(lower, "sit"):
				v.SitMode = mode
			}
		}
	}

	return v
}

func displayName(d *apidevice.Device) string {
	if name := d.GetConfig().GetName(); name != "" {
		return name
	}
	return d.GetId()
}

func deviceOnOff(d *apidevice.Device) *apitrait.OnOff {
	switch {
	case d.GetLight() != nil:
		return d.GetLight().GetOnOff()
	case d.GetFan() != nil:
		return d.GetFan().GetOnOff()
	case d.GetAvReceiver() != nil:
		return d.GetAvReceiver().GetOnOff()
	case d.GetClock() != nil:
		return d.GetClock().GetOnOff()
	case d.GetEvCharger() != nil:
		return d.GetEvCharger().GetOnOff()
	case d.GetUps() != nil:
		return d.GetUps().GetOnOff()
	case d.GetTelevision() != nil:
		return d.GetTelevision().GetOnOff()
	case d.GetThermostat() != nil:
		return d.GetThermostat().GetOnOff()
	case d.GetGeneric() != nil:
		return d.GetGeneric().GetOnOff()
	default:
		return nil
	}
}

func deviceBrightness(d *apidevice.Device) *apitrait.Brightness {
	switch {
	case d.GetLight() != nil:
		return d.GetLight().GetBrightness()
	case d.GetClock() != nil:
		return d.GetClock().GetBrightness()
	case d.GetGeneric() != nil:
		return d.GetGeneric().GetBrightness()
	default:
		return nil
	}
}

func deviceColour(d *apidevice.Device) *apitrait.Colour {
	switch {
	case d.GetLight() != nil:
		return d.GetLight().GetColour()
	case d.GetGeneric() != nil:
		return d.GetGeneric().GetColour()
	default:
		return nil
	}
}

func deviceSpeed(d *apidevice.Device) *apitrait.Speed {
	switch {
	case d.GetFan() != nil:
		return d.GetFan().GetSpeed()
	case d.GetGeneric() != nil:
		return d.GetGeneric().GetSpeed()
	default:
		return nil
	}
}

func deviceMode(d *apidevice.Device) *apitrait.Mode {
	switch {
	case d.GetStandingDesk() != nil:
		return d.GetStandingDesk().GetMode()
	default:
		return nil
	}
}

// rgbToHSB converts 0-255 RGB to the Hue (0-360 degrees)/Saturation/
// Brightness (0-100 percent) shape command.Colour_Hsb expects, for
// commanding an HSB-mode device from the control screen's fixed RGB preset
// palette.
func rgbToHSB(r, g, b int32) (hue, saturation, brightness int32) {
	rf, gf, bf := float64(r)/255, float64(g)/255, float64(b)/255
	cmax := math.Max(rf, math.Max(gf, bf))
	cmin := math.Min(rf, math.Min(gf, bf))
	delta := cmax - cmin

	var h float64
	switch {
	case delta == 0:
		h = 0
	case cmax == rf:
		h = 60 * math.Mod((gf-bf)/delta, 6)
	case cmax == gf:
		h = 60 * ((bf-rf)/delta + 2)
	default:
		h = 60 * ((rf-gf)/delta + 4)
	}
	if h < 0 {
		h += 360
	}

	s := 0.0
	if cmax != 0 {
		s = delta / cmax
	}

	return int32(math.Round(h)), int32(math.Round(s * 100)), int32(math.Round(cmax * 100))
}

// hsbToRGB converts Hue (0-360 degrees)/Saturation/Brightness (0-100
// percent) to 0-255 RGB, so an HSB-mode device's current colour can be
// compared against the control screen's fixed RGB preset palette (e.g. to
// highlight the active swatch).
func hsbToRGB(hue, saturation, brightness int32) (r, g, b int32) {
	h := float64(hue)
	s := float64(saturation) / 100
	v := float64(brightness) / 100

	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c

	var rf, gf, bf float64
	switch {
	case h < 60:
		rf, gf, bf = c, x, 0
	case h < 120:
		rf, gf, bf = x, c, 0
	case h < 180:
		rf, gf, bf = 0, c, x
	case h < 240:
		rf, gf, bf = 0, x, c
	case h < 300:
		rf, gf, bf = x, 0, c
	default:
		rf, gf, bf = c, 0, x
	}

	return int32(math.Round((rf + m) * 255)), int32(math.Round((gf + m) * 255)), int32(math.Round((bf + m) * 255))
}
