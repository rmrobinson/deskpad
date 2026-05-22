package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
)

type apiTestScreen struct {
	name        string
	showKeys    []image.Image
	pressedKey  int
	pressedType deskpad.KeyPressType
	action      deskpad.KeyPressAction
}

type apiTestMediaPlayer struct {
	playing bool
	item    *ui.MediaItem
	id      string
}

func (p apiTestMediaPlayer) IsPlaying() bool {
	return p.playing
}

func (p apiTestMediaPlayer) CurrentlyPlaying() *ui.MediaItem {
	return p.item
}

func (p apiTestMediaPlayer) ID() string {
	return p.id
}

func (s *apiTestScreen) Name() string {
	return s.name
}

func (s *apiTestScreen) Show() []image.Image {
	return s.showKeys
}

func (s *apiTestScreen) Icon() image.Image {
	return apiTestImage()
}

func (s *apiTestScreen) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	s.pressedKey = id
	s.pressedType = t
	return s.action, nil
}

func TestUIStateReturnsCurrentScreenGridAndKeys(t *testing.T) {
	screen := &apiTestScreen{name: "home", showKeys: []image.Image{apiTestImage(), nil}}
	deck := deskpad.NewDeck(screen)
	web := deskpad.NewWebSurface()
	deck.RegisterSurface(web)
	deck.RefreshScreen()
	api := &API{d: deck, web: web}

	req := httptest.NewRequest(http.MethodGet, "/api/ui/state", nil)
	rec := httptest.NewRecorder()
	api.UIState(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var resp UIStateResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %s", err)
	}

	if resp.CurrentScreen.Name != "home" {
		t.Fatalf("screen = %q, want home", resp.CurrentScreen.Name)
	}
	if resp.Grid.Rows != 3 || resp.Grid.Columns != 5 {
		t.Fatalf("grid = %dx%d, want 3x5", resp.Grid.Rows, resp.Grid.Columns)
	}
	if len(resp.Keys) != deck.KeyCount() {
		t.Fatalf("keys length = %d, want %d", len(resp.Keys), deck.KeyCount())
	}
	if resp.Keys[0] == nil || !strings.HasPrefix(*resp.Keys[0], "data:image/png;base64,") {
		t.Fatalf("key 0 did not contain a png data url")
	}
	if resp.Keys[1] != nil {
		t.Fatalf("key 1 = %v, want nil", *resp.Keys[1])
	}
}

func TestStatusReturnsMediaPlayerDetails(t *testing.T) {
	screen := &apiTestScreen{name: "home"}
	deck := deskpad.NewDeck(screen)
	api := &API{
		d: deck,
		mpc: apiTestMediaPlayer{
			playing: true,
			id:      "spotify",
			item: &ui.MediaItem{
				ID:           "track-1",
				Title:        "Song Title",
				Artists:      []string{"Artist One", "Artist Two"},
				AlbumName:    "Album Name",
				AlburmArtURL: "https://example.test/album.png",
			},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rec := httptest.NewRecorder()
	api.Status(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var body map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %s", err)
	}
	if _, ok := body["mediaPlayer"]; !ok {
		t.Fatalf("response missing mediaPlayer key: %s", rec.Body.String())
	}
	if _, ok := body["MediaPlayer"]; ok {
		t.Fatalf("response includes legacy MediaPlayer key: %s", rec.Body.String())
	}

	var resp StatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal status response: %s", err)
	}

	if resp.MediaPlayer.State != "Playing" {
		t.Fatalf("media state = %q, want Playing", resp.MediaPlayer.State)
	}
	if resp.MediaPlayer.CurrentlyPlaying == nil {
		t.Fatalf("currentlyPlaying is nil")
	}
	if resp.MediaPlayer.CurrentlyPlaying.Title != "Song Title" {
		t.Fatalf("title = %q, want Song Title", resp.MediaPlayer.CurrentlyPlaying.Title)
	}
	if resp.MediaPlayer.CurrentlyPlaying.AlbumArtURL != "https://example.test/album.png" {
		t.Fatalf("album art = %q, want URL", resp.MediaPlayer.CurrentlyPlaying.AlbumArtURL)
	}
}

func TestUIEventsReceivesInitialSnapshotAndUpdate(t *testing.T) {
	updated := apiTestImage()
	screen := &apiTestScreen{
		name:     "home",
		showKeys: []image.Image{nil},
		action:   deskpad.KeyPressAction{Action: deskpad.KeyPressActionUpdateIcon, NewIcon: updated},
	}
	deck := deskpad.NewDeck(screen)
	web := deskpad.NewWebSurface()
	deck.RegisterSurface(web)
	deck.RefreshScreen()
	api := &API{d: deck, web: web}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/ui/events", api.UIEvents)
	server := httptest.NewServer(mux)
	defer server.Close()

	resp, err := http.Get(server.URL + "/api/ui/events")
	if err != nil {
		t.Fatalf("get events: %s", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	initial := readSSEData(t, scanner)
	if !strings.Contains(initial, `"name":"home"`) {
		t.Fatalf("initial event = %q, want home snapshot", initial)
	}

	if err := deck.PressKey(context.Background(), 0, deskpad.KeyPressShort); err != nil {
		t.Fatalf("PressKey returned error: %s", err)
	}

	update := readSSEData(t, scanner)
	if !strings.Contains(update, "data:image/png;base64,") {
		t.Fatalf("update event = %q, want encoded image", update)
	}
}

func TestUIPressKeyRejectsMissingOrWrongBearerToken(t *testing.T) {
	screen := &apiTestScreen{name: "home"}
	deck := deskpad.NewDeck(screen)
	api := &API{d: deck, web: deskpad.NewWebSurface(), authToken: "secret"}

	for _, tc := range []struct {
		name   string
		header string
	}{
		{name: "missing"},
		{name: "wrong", header: "Bearer wrong"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/ui/keys/0/press", strings.NewReader(`{"type":"short"}`))
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			api.UIPressKey(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
		})
	}
}

func TestUIPressKeyRejectsWritesWhenTokenUnset(t *testing.T) {
	screen := &apiTestScreen{name: "home"}
	deck := deskpad.NewDeck(screen)
	api := &API{d: deck, web: deskpad.NewWebSurface()}

	req := httptest.NewRequest(http.MethodPost, "/api/ui/keys/0/press", strings.NewReader(`{"type":"short"}`))
	rec := httptest.NewRecorder()
	api.UIPressKey(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestUIPressKeyAcceptsValidTokenAndPressesDeck(t *testing.T) {
	screen := &apiTestScreen{name: "home", action: deskpad.KeyPressAction{Action: deskpad.KeyPressActionNoop}}
	deck := deskpad.NewDeck(screen)
	api := &API{d: deck, web: deskpad.NewWebSurface(), authToken: "secret"}

	req := httptest.NewRequest(http.MethodPost, "/api/ui/keys/7/press", bytes.NewBufferString(`{"type":"long"}`))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	api.UIPressKey(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204: %s", rec.Code, rec.Body.String())
	}
	if screen.pressedKey != 7 {
		t.Fatalf("pressed key = %d, want 7", screen.pressedKey)
	}
	if screen.pressedType != deskpad.KeyPressLong {
		t.Fatalf("pressed type = %d, want long", screen.pressedType)
	}
}

func readSSEData(t *testing.T, scanner *bufio.Scanner) string {
	t.Helper()

	deadline := time.After(2 * time.Second)
	lines := make(chan string, 1)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				lines <- strings.TrimPrefix(line, "data: ")
				return
			}
		}
		lines <- ""
	}()

	select {
	case line := <-lines:
		if line == "" {
			t.Fatalf("did not read SSE data line: %s", scanner.Err())
		}
		return line
	case <-deadline:
		t.Fatal("timed out waiting for SSE data")
		return ""
	}
}

func apiTestImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	return img
}
