package controllers

import (
	"context"
	"image"
	"log"
	"net/http"

	"github.com/rmrobinson/deskpad/service"
	"github.com/rmrobinson/go-mpris"
	"github.com/zmb3/spotify/v2"
	_ "golang.org/x/image/webp"
)

// MediaPlaylist is a controller which manages media playlist management
type MediaPlaylist struct {
	spotifyClient *spotify.Client
	mprisClient   *mpris.Player

	cachedPlaylists []service.MediaPlaylist
	currentPlaylist *service.MediaPlaylist
}

// NewMediaPlaylist creates a controller for media playlist management. If supplied, MPRIS will be used for playback;
// however Spotify is currently always used as the source of media content.
func NewMediaPlaylist(sc *spotify.Client, mc *mpris.Player) *MediaPlaylist {
	return &MediaPlaylist{
		spotifyClient: sc,
		mprisClient:   mc,
	}
}

// GetPlaylists retrieves the list of cached playlists.
func (mp *MediaPlaylist) GetPlaylists(count int, offset int) []service.MediaPlaylist {
	startIdx := offset
	if startIdx > len(mp.cachedPlaylists) {
		startIdx = 0
	}

	endIdx := startIdx + count
	if endIdx > len(mp.cachedPlaylists) {
		endIdx = len(mp.cachedPlaylists)
	}

	return mp.cachedPlaylists[startIdx:endIdx]

}

// RefreshPlaylists retrieves an up-to-date list of playlists. This can be run on a schedule to ensure content is up-to-date.
func (mp *MediaPlaylist) RefreshPlaylists(ctx context.Context) error {
	mediaPlaylists := []service.MediaPlaylist{}

	playlists, err := mp.spotifyClient.CurrentUsersPlaylists(ctx, spotify.Limit(50))
	if err != nil {
		log.Printf("error getting current playlists: %s\n", err.Error())
		return err
	}

	for _, playlist := range playlists.Playlists {
		log.Printf("got playlist %s with name %s\n", playlist.URI, playlist.Name)
		mediaPlaylist := service.MediaPlaylist{ID: string(playlist.URI), Name: playlist.Name}
		if len(playlist.Images) > 0 {
			imgURL := playlist.Images[0].URL
			resp, err := http.Get(imgURL)
			if err != nil {
				log.Printf("unable to download %s: %s\n", imgURL, err.Error())
				continue
			}
			defer resp.Body.Close()

			mediaPlaylist.Icon, _, err = image.Decode(resp.Body)
			if err != nil {
				log.Printf("unable to decode image for playlist %s at %s: %s\n", playlist.Name, imgURL, err.Error())
				continue
			}
		} else {
			log.Printf("playlist %s has no image\n", playlist.Name)
		}

		mediaPlaylists = append(mediaPlaylists, mediaPlaylist)
	}

	log.Printf("caching %d playlists\n", len(mediaPlaylists))
	mp.cachedPlaylists = mediaPlaylists
	return nil
}

// StartPlaylist begins playing the requested playlist URI. If the MPRIS client is supplied it will use it; otherwise it'll default to Spotify.
func (mp *MediaPlaylist) StartPlaylist(ctx context.Context, id string) {
	if mp.mprisClient != nil {
		log.Printf("playing URI: %s\n", id)
		mp.mprisClient.OpenUri(id)
		mp.currentPlaylist = mp.getPlaylistbyID(id)
	} else {
		playlistURI := spotify.URI(id)
		playlistOffset := 0
		opts := &spotify.PlayOptions{
			PlaybackContext: &playlistURI,
			PlaybackOffset:  &spotify.PlaybackOffset{Position: &playlistOffset},
		}

		if err := mp.spotifyClient.PlayOpt(ctx, opts); err != nil {
			log.Printf("error playing new playlist: %s\n", err.Error())
			mp.currentPlaylist = nil
			return
		}

		log.Printf("playing URI: %s\n", id)
		mp.currentPlaylist = mp.getPlaylistbyID(id)
	}
}

func (mp *MediaPlaylist) getPlaylistbyID(id string) *service.MediaPlaylist {
	for _, p := range mp.cachedPlaylists {
		if p.ID == id {
			return &p
		}
	}

	return nil
}
