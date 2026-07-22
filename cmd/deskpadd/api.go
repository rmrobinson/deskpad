package main

import (
	"bytes"
	"embed"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/deskpad/ui/controllers"
)

//go:embed web/index.html web/manifest.webmanifest web/service-worker.js web/icons/*.png
var webFiles embed.FS

type MediaItem struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Artists     []string `json:"artists"`
	AlbumName   string   `json:"albumName"`
	AlbumArtURL string   `json:"albumArtUrl"`
}
type MediaPlaylist struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type AudioOutput struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Volume      int    `json:"volume"`
	Muted       bool   `json:"muted"`
	Active      bool   `json:"active"`
}

type StatusResponse struct {
	Audio struct {
		Outputs         []AudioOutput `json:"outputs"`
		DefaultOutputID string        `json:"defaultOutputId"`
	} `json:"audio"`
	MediaPlayer struct {
		State            string         `json:"state"`
		CurrentlyPlaying *MediaItem     `json:"currentlyPlaying"`
		CurrentPlaylist  *MediaPlaylist `json:"currentPlaylist"`
		ID               string         `json:"id"`
	} `json:"mediaPlayer"`
	UI struct {
		CurrentScreen struct {
			Name string `json:"name"`
		} `json:"currentScreen"`
		StreamdeckID string `json:"streamDeckId"`
	} `json:"ui"`
	// TODO: add playlists
}

type UIStateResponse struct {
	CurrentScreen struct {
		Name string `json:"name"`
	} `json:"currentScreen"`
	Grid struct {
		Rows    int `json:"rows"`
		Columns int `json:"columns"`
	} `json:"grid"`
	Keys []*string `json:"keys"`
}

type MediaPlayerController interface {
	IsPlaying() bool
	CurrentlyPlaying() *ui.MediaItem
	ID() string
}

type API struct {
	mpc  MediaPlayerController
	mplc *controllers.MediaPlaylist
	mpsc *controllers.MediaPlayerSetting

	d         *deskpad.Deck
	web       *deskpad.WebSurface
	authToken string
}

func (a *API) Status(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/status" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		resp := &StatusResponse{}

		resp.UI.CurrentScreen.Name = a.d.Screen().Name()
		resp.UI.StreamdeckID = a.d.ID()

		if a.mpc != nil {
			isPlaying := a.mpc.IsPlaying()
			if isPlaying {
				resp.MediaPlayer.State = "Playing"
			} else {
				resp.MediaPlayer.State = "Not Playing"
			}
			resp.MediaPlayer.ID = a.mpc.ID()
		}

		if a.mpsc != nil {
			outputs := a.mpsc.GetAudioOutputs()

			for _, output := range outputs {
				resp.Audio.Outputs = append(resp.Audio.Outputs, AudioOutput{
					ID:          output.ID,
					Name:        output.Name,
					Description: output.Description,
					Muted:       output.Muted,
					Active:      output.Active,
				})
			}
		}

		if a.mpc != nil {
			currentlyPlaying := a.mpc.CurrentlyPlaying()
			if currentlyPlaying != nil {
				resp.MediaPlayer.CurrentlyPlaying = &MediaItem{
					ID:          currentlyPlaying.ID,
					Title:       currentlyPlaying.Title,
					Artists:     currentlyPlaying.Artists,
					AlbumName:   currentlyPlaying.AlbumName,
					AlbumArtURL: currentlyPlaying.AlburmArtURL,
				}
			}
		}
		if a.mplc != nil {
			currentPlaylist := a.mplc.CurrentlyPlaylist()
			if currentPlaylist != nil {
				resp.MediaPlayer.CurrentPlaylist = &MediaPlaylist{
					ID:   currentPlaylist.ID,
					Name: currentPlaylist.Name,
				}
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case http.MethodOptions:
		w.Header().Set("Allow", "GET, OPTIONS")
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", "GET, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (a *API) Index(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	serveEmbeddedFile(w, "web/index.html")
}

func (a *API) WebAsset(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/manifest.webmanifest":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/manifest+json")
		serveEmbeddedFile(w, "web/manifest.webmanifest")

	case r.URL.Path == "/service-worker.js":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Service-Worker-Allowed", "/")
		serveEmbeddedFile(w, "web/service-worker.js")

	case strings.HasPrefix(r.URL.Path, "/icons/"):
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		webRoot, err := fs.Sub(webFiles, "web")
		if err != nil {
			http.Error(w, "unable to load web assets", http.StatusInternalServerError)
			return
		}
		http.FileServer(http.FS(webRoot)).ServeHTTP(w, r)

	default:
		http.NotFound(w, r)
	}
}

func (a *API) UIState(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/ui/state" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, snapshotToUIState(a.web.Snapshot()))
}

func (a *API) UIEvents(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/ui/events" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	events, cancel := a.web.Subscribe()
	defer cancel()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case snapshot, ok := <-events:
			if !ok {
				return
			}

			data, err := json.Marshal(snapshotToUIState(snapshot))
			if err != nil {
				log.Printf("unable to marshal ui state event: %s\n", err.Error())
				continue
			}

			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (a *API) UIPressKey(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/api/ui/keys/") || !strings.HasSuffix(r.URL.Path, "/press") {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !a.authorized(r) {
		if a.authToken == "" {
			log.Printf("web writes disabled: web.auth-token is empty\n")
			http.Error(w, "web writes disabled", http.StatusForbidden)
			return
		}

		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	idPart := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/ui/keys/"), "/press")
	keyID, err := strconv.Atoi(idPart)
	if err != nil || keyID < 0 || keyID >= a.d.KeyCount() {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}

	var req struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	var pressType deskpad.KeyPressType
	switch req.Type {
	case "short":
		pressType = deskpad.KeyPressShort
	case "long":
		pressType = deskpad.KeyPressLong
	default:
		http.Error(w, "invalid press type", http.StatusBadRequest)
		return
	}

	log.Printf("web key press received: key=%d type=%s screen=%q\n", keyID, req.Type, a.d.Screen().Name())
	if err := a.d.PressKey(r.Context(), keyID, pressType); err != nil {
		log.Printf("web key press failed: key=%d type=%s error=%s\n", keyID, req.Type, err.Error())
		http.Error(w, "press failed", http.StatusInternalServerError)
		return
	}
	log.Printf("web key press handled: key=%d type=%s\n", keyID, req.Type)

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) authorized(r *http.Request) bool {
	if a.authToken == "" {
		return false
	}

	return r.Header.Get("Authorization") == "Bearer "+a.authToken
}

func snapshotToUIState(snapshot deskpad.Snapshot) UIStateResponse {
	var resp UIStateResponse
	resp.CurrentScreen.Name = snapshot.ScreenName
	resp.Grid.Rows = snapshot.Rows
	resp.Grid.Columns = snapshot.Columns
	resp.Keys = make([]*string, len(snapshot.Keys))

	for i, key := range snapshot.Keys {
		if key == nil {
			continue
		}

		dataURL, err := imageDataURL(key)
		if err != nil {
			log.Printf("unable to encode key %d image: %s\n", i, err.Error())
			continue
		}
		resp.Keys[i] = &dataURL
	}

	return resp
}

func imageDataURL(img image.Image) (string, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func serveEmbeddedFile(w http.ResponseWriter, name string) {
	data, err := webFiles.ReadFile(name)
	if err != nil {
		http.Error(w, "unable to load web asset", http.StatusInternalServerError)
		return
	}

	w.Write(data)
}
