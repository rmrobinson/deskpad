package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
)

// fakeDevice records what the bridge does to a Stream Deck and lets a test press keys.
type fakeDevice struct {
	keys   int
	events chan sdeck.Event

	mu         sync.Mutex
	filled     map[int]int // key -> times filled
	cleared    map[int]int
	allCleared int
	fillErr    error
}

func newFakeDevice(keys int) *fakeDevice {
	return &fakeDevice{keys: keys, events: make(chan sdeck.Event, 16), filled: map[int]int{}, cleared: map[int]int{}}
}

func (f *fakeDevice) NumKeys() int                  { return f.keys }
func (f *fakeDevice) Subscribe() <-chan sdeck.Event { return f.events }

func (f *fakeDevice) FillImage(key int, _ image.Image) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fillErr != nil {
		return f.fillErr
	}
	f.filled[key]++
	return nil
}

func (f *fakeDevice) ClearKey(key int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared[key]++
	return nil
}

func (f *fakeDevice) ClearAllKeys() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.allCleared++
	return nil
}

func (f *fakeDevice) snapshot() (filled, cleared map[int]int, all int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	filled, cleared = map[int]int{}, map[int]int{}
	for k, v := range f.filled {
		filled[k] = v
	}
	for k, v := range f.cleared {
		cleared[k] = v
	}
	return filled, cleared, f.allCleared
}

func keyImage(t *testing.T, c color.Color) *string {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 72, 72))
	for x := 0; x < 72; x++ {
		for y := 0; y < 72; y++ {
			img.Set(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}

	s := pngDataURLPrefix + base64.StdEncoding.EncodeToString(buf.Bytes())
	return &s
}

func stateWith(keys ...*string) uiState {
	var s uiState
	s.CurrentScreen.Name = "home"
	s.Grid.Rows, s.Grid.Columns = 3, 5
	s.Keys = keys
	return s
}

func TestDecodeKeyImage(t *testing.T) {
	good := keyImage(t, color.RGBA{R: 255, A: 255})
	img, err := decodeKeyImage(*good)
	if err != nil {
		t.Fatalf("decodeKeyImage: %s", err)
	}
	if b := img.Bounds(); b.Dx() != 72 || b.Dy() != 72 {
		t.Errorf("decoded %v, want 72x72", b)
	}

	for _, bad := range []string{"", "data:image/jpeg;base64,AAAA", pngDataURLPrefix + "!!!", pngDataURLPrefix + "AAAA"} {
		if _, err := decodeKeyImage(bad); err == nil {
			t.Errorf("decodeKeyImage(%q) succeeded, want an error", bad)
		}
	}
}

func TestApplyOnlyWritesKeysThatChanged(t *testing.T) {
	dev := newFakeDevice(4)
	b := newBridge(dev, "http://unused", "")

	red, green := keyImage(t, color.RGBA{R: 255, A: 255}), keyImage(t, color.RGBA{G: 255, A: 255})

	if err := b.apply(stateWith(red, nil, green)); err != nil {
		t.Fatal(err)
	}
	filled, cleared, _ := dev.snapshot()
	if filled[0] != 1 || filled[2] != 1 || len(filled) != 2 {
		t.Errorf("first apply filled %v, want keys 0 and 2 once", filled)
	}
	if cleared[1] != 1 || cleared[3] != 1 {
		t.Errorf("first apply cleared %v, want the blank keys 1 and 3", cleared)
	}

	// The same state again must not touch the device.
	if err := b.apply(stateWith(red, nil, green)); err != nil {
		t.Fatal(err)
	}
	filled, cleared, _ = dev.snapshot()
	if filled[0] != 1 || filled[2] != 1 || cleared[1] != 1 {
		t.Errorf("unchanged state rewrote keys: filled %v cleared %v", filled, cleared)
	}

	// One key changing writes only that key.
	if err := b.apply(stateWith(red, green, green)); err != nil {
		t.Fatal(err)
	}
	filled, _, _ = dev.snapshot()
	if filled[1] != 1 || filled[0] != 1 || filled[2] != 1 {
		t.Errorf("after one change filled %v, want only key 1 added", filled)
	}

	// A key going blank is cleared.
	if err := b.apply(stateWith(red, green, nil)); err != nil {
		t.Fatal(err)
	}
	_, cleared, _ = dev.snapshot()
	if cleared[2] != 1 {
		t.Errorf("key 2 went blank but cleared = %v", cleared)
	}
}

func TestApplyIgnoresKeysBeyondTheDevice(t *testing.T) {
	dev := newFakeDevice(2)
	b := newBridge(dev, "http://unused", "")

	img := keyImage(t, color.RGBA{B: 255, A: 255})
	if err := b.apply(stateWith(img, img, img, img)); err != nil {
		t.Fatal(err)
	}

	filled, _, _ := dev.snapshot()
	if len(filled) != 2 {
		t.Errorf("filled %v, want only the device's 2 keys", filled)
	}
}

func TestApplyBadImageBlanksKeyWithoutFailing(t *testing.T) {
	dev := newFakeDevice(2)
	b := newBridge(dev, "http://unused", "")

	bad := "data:image/png;base64,AAAA"
	if err := b.apply(stateWith(&bad)); err != nil {
		t.Fatalf("apply failed for a bad image: %s", err)
	}

	_, cleared, _ := dev.snapshot()
	if cleared[0] == 0 {
		t.Errorf("bad image should leave key 0 blank, cleared = %v", cleared)
	}
}

func TestApplyReturnsDeviceErrors(t *testing.T) {
	dev := newFakeDevice(2)
	dev.fillErr = errors.New("hid: device gone")
	b := newBridge(dev, "http://unused", "")

	err := b.apply(stateWith(keyImage(t, color.RGBA{A: 255})))

	var devErr *deviceError
	if !errors.As(err, &devErr) {
		t.Fatalf("apply error = %v, want a deviceError", err)
	}
}

func TestDisconnectedBlanksKeysAndForgetsState(t *testing.T) {
	dev := newFakeDevice(2)
	b := newBridge(dev, "http://unused", "")
	img := keyImage(t, color.RGBA{R: 1, A: 255})

	_ = b.apply(stateWith(img))
	b.disconnected()

	if _, _, all := dev.snapshot(); all != 1 {
		t.Errorf("ClearAllKeys called %d times, want 1", all)
	}

	// After a disconnect the same image must be written again.
	_ = b.apply(stateWith(img))
	filled, _, _ := dev.snapshot()
	if filled[0] != 2 {
		t.Errorf("key 0 filled %d times, want 2 (once per connection)", filled[0])
	}
}

type pressRecord struct {
	path, auth, kind string
}

func newPressServer(t *testing.T, got chan<- pressRecord) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Type string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		got <- pressRecord{path: r.URL.Path, auth: r.Header.Get("Authorization"), kind: body.Type}
		w.WriteHeader(http.StatusNoContent)
	}))
}

func TestPressKeyRequest(t *testing.T) {
	got := make(chan pressRecord, 1)
	srv := newPressServer(t, got)
	defer srv.Close()

	if err := pressKey(context.Background(), srv.Client(), srv.URL, "s3cret", 7, "long"); err != nil {
		t.Fatal(err)
	}

	r := <-got
	if r.path != "/api/ui/keys/7/press" || r.auth != "Bearer s3cret" || r.kind != "long" {
		t.Errorf("request = %+v", r)
	}
}

func TestPressKeyReportsRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	if err := pressKey(context.Background(), srv.Client(), srv.URL, "wrong", 0, "short"); err == nil {
		t.Errorf("pressKey succeeded against a 401")
	}
}

func TestKeyEventsBecomeShortAndLongPresses(t *testing.T) {
	got := make(chan pressRecord, 4)
	srv := newPressServer(t, got)
	defer srv.Close()

	dev := newFakeDevice(15)
	b := newBridge(dev, srv.URL, "tok")
	b.client = srv.Client()
	b.longPress = 80 * time.Millisecond
	b.setConnected(true)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.handleKeys(ctx)

	// A quick tap on key 3.
	dev.events <- sdeck.Event{Key: 3, Type: sdeck.EventTypeDown}
	dev.events <- sdeck.Event{Key: 3, Type: sdeck.EventTypeUp}
	// A held press on key 9.
	dev.events <- sdeck.Event{Key: 9, Type: sdeck.EventTypeDown}
	time.Sleep(200 * time.Millisecond)
	dev.events <- sdeck.Event{Key: 9, Type: sdeck.EventTypeUp}

	var presses []pressRecord
	for len(presses) < 2 {
		select {
		case r := <-got:
			presses = append(presses, r)
		case <-time.After(3 * time.Second):
			t.Fatalf("got %d presses, want 2: %+v", len(presses), presses)
		}
	}

	if presses[0].path != "/api/ui/keys/3/press" || presses[0].kind != "short" {
		t.Errorf("first press = %+v, want a short press of key 3", presses[0])
	}
	if presses[1].path != "/api/ui/keys/9/press" || presses[1].kind != "long" {
		t.Errorf("second press = %+v, want a long press of key 9", presses[1])
	}
}

func TestPressesAreDroppedWhileDisconnected(t *testing.T) {
	var requests int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&requests, 1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	dev := newFakeDevice(15)
	b := newBridge(dev, srv.URL, "tok")
	b.client = srv.Client()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go b.handleKeys(ctx)

	dev.events <- sdeck.Event{Key: 1, Type: sdeck.EventTypeDown}
	dev.events <- sdeck.Event{Key: 1, Type: sdeck.EventTypeUp}
	time.Sleep(150 * time.Millisecond)

	if n := atomic.LoadInt32(&requests); n != 0 {
		t.Errorf("sent %d requests while disconnected, want 0", n)
	}
}

func sseServer(t *testing.T, writeEvents func(w http.ResponseWriter, f http.Flusher, r *http.Request)) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/ui/state":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"currentScreen":{"name":"home"},"grid":{"rows":3,"columns":5},"keys":[null]}`)
		case "/api/ui/events":
			w.Header().Set("Content-Type", "text/event-stream")
			f := w.(http.Flusher)
			writeEvents(w, f, r)
		}
	}))
}

func TestStreamStatesParsesEventsAndSkipsHeartbeats(t *testing.T) {
	srv := sseServer(t, func(w http.ResponseWriter, f http.Flusher, r *http.Request) {
		fmt.Fprint(w, ": heartbeat\n\n")
		fmt.Fprint(w, `data: {"currentScreen":{"name":"media"},"grid":{"rows":3,"columns":5},"keys":["data:image/png;base64,AAAA",null]}`+"\n\n")
		fmt.Fprint(w, ": heartbeat\n\n")
		fmt.Fprint(w, `data: {"currentScreen":{"name":"home"},"grid":{"rows":3,"columns":5},"keys":[]}`+"\n\n")
		f.Flush()
	})
	defer srv.Close()

	var screens []string
	err := streamStates(context.Background(), srv.Client(), srv.URL, time.Minute, func(s uiState) {
		screens = append(screens, s.CurrentScreen.Name)
	})

	if err == nil {
		t.Errorf("streamStates returned nil when the server closed the stream")
	}
	if strings.Join(screens, ",") != "media,home" {
		t.Errorf("states = %v, want [media home]", screens)
	}
}

func TestStreamStatesGivesUpOnSilence(t *testing.T) {
	srv := sseServer(t, func(w http.ResponseWriter, f http.Flusher, r *http.Request) {
		f.Flush()
		<-r.Context().Done() // never send anything, not even a heartbeat
	})
	defer srv.Close()

	start := time.Now()
	err := streamStates(context.Background(), srv.Client(), srv.URL, 150*time.Millisecond, func(uiState) {})

	if err == nil {
		t.Fatalf("streamStates returned nil on a silent stream")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("took %s to notice silence, want about the idle timeout", elapsed)
	}
}

func TestRunReconnectsAfterTheDaemonComesBack(t *testing.T) {
	var attempts int32
	img := keyImage(t, color.RGBA{R: 9, A: 255})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ui/state" {
			if atomic.AddInt32(&attempts, 1) < 3 {
				http.Error(w, "starting", http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintf(w, `{"currentScreen":{"name":"home"},"grid":{"rows":3,"columns":5},"keys":["%s"]}`, *img)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer srv.Close()

	dev := newFakeDevice(15)
	b := newBridge(dev, srv.URL, "")
	b.client = srv.Client()
	b.minBackoff, b.maxBackoff = 10*time.Millisecond, 20*time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- b.Run(ctx) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if filled, _, _ := dev.snapshot(); filled[0] == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if filled, _, _ := dev.snapshot(); filled[0] != 1 {
		t.Fatalf("key 0 was never painted after the daemon came back (attempts: %d)", atomic.LoadInt32(&attempts))
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run returned %v on shutdown, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not stop after the context was cancelled")
	}

	if _, _, all := dev.snapshot(); all == 0 {
		t.Errorf("keys were not blanked on shutdown")
	}
}

func TestRunReturnsDeviceErrors(t *testing.T) {
	img := keyImage(t, color.RGBA{R: 2, A: 255})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/ui/state" {
			fmt.Fprintf(w, `{"keys":["%s"]}`, *img)
			return
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	dev := newFakeDevice(15)
	dev.fillErr = errors.New("hid: write failed")
	b := newBridge(dev, srv.URL, "")
	b.client = srv.Client()

	done := make(chan error, 1)
	go func() { done <- b.Run(context.Background()) }()

	select {
	case err := <-done:
		var devErr *deviceError
		if !errors.As(err, &devErr) {
			t.Errorf("Run error = %v, want a deviceError so the process restarts", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("Run did not return after a device error")
	}
}

func TestNormalizeServer(t *testing.T) {
	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{"http://h033:1338", "http://h033:1338", false},
		{"  http://h033:1338/  ", "http://h033:1338", false},
		{"https://deskpad.example", "https://deskpad.example", false},
		{"", "", true},
		{"h033:1338", "", true},
		{"ftp://h033", "", true},
	}

	for _, tt := range tests {
		got, err := normalizeServer(tt.in)
		if (err != nil) != tt.wantErr || got != tt.want {
			t.Errorf("normalizeServer(%q) = (%q, %v), want (%q, err=%t)", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

// TestAgainstLiveDaemon checks the wire format against a real deskpadd. It only reads (state and one event), and is
// skipped unless DESKPAD_SMOKE_SERVER is set, e.g. DESKPAD_SMOKE_SERVER=http://h033:1338.
func TestAgainstLiveDaemon(t *testing.T) {
	server := os.Getenv("DESKPAD_SMOKE_SERVER")
	if server == "" {
		t.Skip("DESKPAD_SMOKE_SERVER not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	state, err := fetchState(ctx, http.DefaultClient, server)
	if err != nil {
		t.Fatalf("fetchState: %s", err)
	}
	if state.Grid.Rows == 0 || state.Grid.Columns == 0 || len(state.Keys) != state.Grid.Rows*state.Grid.Columns {
		t.Fatalf("grid %dx%d with %d keys", state.Grid.Rows, state.Grid.Columns, len(state.Keys))
	}

	imaged := 0
	for i, k := range state.Keys {
		if k == nil {
			continue
		}
		img, err := decodeKeyImage(*k)
		if err != nil {
			t.Fatalf("key %d: %s", i, err)
		}
		if b := img.Bounds(); b.Dx() != 72 || b.Dy() != 72 {
			t.Errorf("key %d is %v, want 72x72", i, b)
		}
		imaged++
	}
	t.Logf("screen %q, %dx%d grid, %d keys with images", state.CurrentScreen.Name, state.Grid.Rows, state.Grid.Columns, imaged)

	// The stream must at least connect and stay open until the heartbeat or the deadline.
	streamCtx, stop := context.WithTimeout(ctx, 2*time.Second)
	defer stop()
	err = streamStates(streamCtx, http.DefaultClient, server, time.Minute, func(uiState) {})
	if err == nil || !strings.Contains(err.Error(), "context deadline exceeded") {
		t.Errorf("streamStates ended with %v, want it to stay open until the deadline", err)
	}
}
