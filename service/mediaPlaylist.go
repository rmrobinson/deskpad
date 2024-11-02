package service

import "image"

// MediaPlaylist contains relevant information about a single playlist, such as it's ID, name and a visual representation of it.
type MediaPlaylist struct {
	ID   string
	Name string
	Icon image.Image
}
