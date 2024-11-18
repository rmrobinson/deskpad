package main

import (
	"encoding/json"
	"net/http"

	"github.com/rmrobinson/deskpad/service"
)

type MediaItem struct {
	Title        string   `json:"title"`
	Artists      []string `json:"artists"`
	AlbumName    string   `json:"albumName"`
	PlaylistName string   `json:"playlistName"`
	AlbumArtURL  string   `json:"albumArtUrl"`
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
		State            string     `json:"state"`
		CurrentlyPlaying *MediaItem `json:"currentlyPlaying"`
	}
	// TODO: add playlists
}

type API struct {
	mpc  service.MediaPlayerController
	mpsc service.MediaPlayerSettingController
}

func (a *API) Status(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/status" {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		resp := &StatusResponse{}

		isPlaying := a.mpc.IsPlaying()
		if isPlaying {
			resp.MediaPlayer.State = "Playing"
		} else {
			resp.MediaPlayer.State = "Not Playing"
		}

		outputs := a.mpsc.GetAudioOutputs(r.Context())

		for _, output := range outputs {
			resp.Audio.Outputs = append(resp.Audio.Outputs, AudioOutput{
				ID:          output.ID,
				Name:        output.Name,
				Description: output.Description,
				Muted:       output.Muted,
				Active:      output.Active,
			})
		}

		currentlyPlaying := a.mpc.CurrentlyPlaying(r.Context())
		if currentlyPlaying != nil {
			resp.MediaPlayer.CurrentlyPlaying = &MediaItem{
				Title:       currentlyPlaying.Title,
				Artists:     currentlyPlaying.Artists,
				AlbumName:   currentlyPlaying.AlbumName,
				AlbumArtURL: currentlyPlaying.AlburmArtURL,
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
