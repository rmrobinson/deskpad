package controllers

import (
	"context"
	"image"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/rmrobinson/deskpad/ui"
	"github.com/zmb3/spotify/v2"
	_ "golang.org/x/image/webp"
)

type PlaylistPlaybackController interface {
	PlayURI(ctx context.Context, uri string) error
}

const playlistPlaybackTimeout = 5 * time.Second

// MediaPlaylist is a controller which manages media playlist management
// There are 2 sources of playlist data: a statically configured list of playlists;
// and a dynamically refreshed list of playlists. These imlementation details are abstracted away
// from the MediaPlaylist functions, which expose just a set of playlists for a UI to consume.
type MediaPlaylist struct {
	lock               sync.RWMutex
	spotifyClient      *spotify.Client
	playbackController PlaylistPlaybackController

	staticPlaylists []ui.MediaPlaylist
	cachedPlaylists []ui.MediaPlaylist

	playlists       []ui.MediaPlaylist
	currentPlaylist *ui.MediaPlaylist
}

// NewMediaPlaylist creates a controller for media playlist management. Spotify is always used
// as the source of media content, and the playback controller is used to launch playlists.
func NewMediaPlaylist(sc *spotify.Client, pc PlaylistPlaybackController, staticPlaylists []ui.MediaPlaylist) *MediaPlaylist {
	return &MediaPlaylist{
		spotifyClient:      sc,
		playbackController: pc,
		staticPlaylists:    staticPlaylists,
		playlists:          staticPlaylists,
	}
}

// GetPlaylists retrieves the list of cached playlists.
func (mp *MediaPlaylist) GetPlaylists(count int, offset int) []ui.MediaPlaylist {
	mp.lock.RLock()
	defer mp.lock.RUnlock()

	startIdx := offset
	if startIdx > len(mp.playlists) {
		startIdx = 0
	}

	endIdx := startIdx + count
	if endIdx > len(mp.playlists) {
		endIdx = len(mp.playlists)
	}

	playlists := make([]ui.MediaPlaylist, endIdx-startIdx)
	copy(playlists, mp.playlists[startIdx:endIdx])
	return playlists

}

// RefreshPlaylists retrieves an up-to-date list of playlists. This can be run on a schedule to ensure content is up-to-date.
func (mp *MediaPlaylist) RefreshPlaylists(ctx context.Context) error {
	mediaPlaylists := []ui.MediaPlaylist{}

	playlists, err := mp.spotifyClient.CurrentUsersPlaylists(ctx, spotify.Limit(50))
	if err != nil {
		log.Printf("error getting current playlists: %s\n", err.Error())
		return err
	}

	for _, playlist := range playlists.Playlists {
		log.Printf("got playlist %s with name %s\n", playlist.URI, playlist.Name)
		if len(playlist.URI) < 1 {
			log.Printf("skipping empty playlist\n")
			continue
		}

		mediaPlaylist := ui.MediaPlaylist{ID: string(playlist.URI), Name: playlist.Name}
		if len(playlist.Images) > 0 {
			imgURL := playlist.Images[0].URL
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
			if err != nil {
				log.Printf("unable to create request for playlist image %s: %s\n", imgURL, err.Error())
				continue
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Printf("unable to download %s: %s\n", imgURL, err.Error())
				continue
			}

			mediaPlaylist.Icon, _, err = image.Decode(resp.Body)
			resp.Body.Close()
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
	mp.lock.Lock()
	defer mp.lock.Unlock()

	mp.cachedPlaylists = mediaPlaylists

	mp.playlists = nil
	mp.playlists = append(mp.playlists, mp.cachedPlaylists...)
	mp.playlists = append(mp.playlists, mp.staticPlaylists...)
	return nil
}

// StartPlaylist begins playing the requested playlist URI.
func (mp *MediaPlaylist) StartPlaylist(ctx context.Context, id string) {
	mp.lock.Lock()
	playbackController := mp.playbackController
	if mp.playbackController == nil {
		log.Printf("no playback controller available for URI: %s\n", id)
		mp.currentPlaylist = nil
		mp.lock.Unlock()
		return
	}

	log.Printf("playing URI: %s\n", id)
	mp.currentPlaylist = mp.getPlaylistbyIDLocked(id)
	mp.lock.Unlock()

	go func() {
		playbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), playlistPlaybackTimeout)
		defer cancel()

		if err := playbackController.PlayURI(playbackCtx, id); err != nil {
			log.Printf("error playing URI %s: %s\n", id, err.Error())
			mp.clearCurrentPlaylistIfID(id)
		}
	}()
}

// CurrentPlaylist returns the currently active playlist, if set.
func (mp *MediaPlaylist) CurrentlyPlaylist() *ui.MediaPlaylist {
	mp.lock.RLock()
	defer mp.lock.RUnlock()

	if mp.currentPlaylist == nil {
		return nil
	}

	currentPlaylist := *mp.currentPlaylist
	return &currentPlaylist
}

func (mp *MediaPlaylist) getPlaylistbyIDLocked(id string) *ui.MediaPlaylist {
	for _, p := range mp.playlists {
		if p.ID == id {
			playlist := p
			return &playlist
		}
	}

	return nil
}

func (mp *MediaPlaylist) clearCurrentPlaylistIfID(id string) {
	mp.lock.Lock()
	defer mp.lock.Unlock()

	if mp.currentPlaylist != nil && mp.currentPlaylist.ID == id {
		mp.currentPlaylist = nil
	}
}
