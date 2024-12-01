package ui

import "image"

// MediaItem contains relevant information about a single media item.
type MediaItem struct {
	ID      string
	Title   string
	Artists []string

	AlbumName    string
	AlburmArtURL string
}

// MediaPlaylist contains relevant information about a single playlist, such as it's ID, name and a visual representation of it.
type MediaPlaylist struct {
	ID   string `mapstructure:"id"`
	Name string `mapstructure:"name"`
	Icon image.Image
}
