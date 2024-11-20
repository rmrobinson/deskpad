package main

import (
	"encoding/json"
	"net/http"

	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/deskpad/ui/controllers"
)

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
	}
	UI struct {
		CurrentScreen struct {
			Name string `json:"name"`
		} `json:"currentScreen"`
	} `json:"ui"`
	// TODO: add playlists
}

type MediaPlayerController interface {
	IsPlaying() bool
	CurrentlyPlaying() *ui.MediaItem
}

type API struct {
	mpc  MediaPlayerController
	mplc *controllers.MediaPlaylist
	mpsc *controllers.MediaPlayerSetting

	d *deskpad.Deck
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

		isPlaying := a.mpc.IsPlaying()
		if isPlaying {
			resp.MediaPlayer.State = "Playing"
		} else {
			resp.MediaPlayer.State = "Not Playing"
		}

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
		currentPlaylist := a.mplc.CurrentlyPlaylist()
		if currentPlaylist != nil {
			resp.MediaPlayer.CurrentPlaylist = &MediaPlaylist{
				ID:   currentPlaylist.ID,
				Name: currentPlaylist.Name,
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)

	case http.MethodOptions:
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		w.WriteHeader(http.StatusNoContent)

	default:
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
