package service

import (
	"context"
)

// MediaPlayerController describes the functions which the screen will use to allow the user to interface with the media source.
type MediaPlayerController interface {
	Play(ctx context.Context)
	Pause(ctx context.Context)
	Next(ctx context.Context)
	Previous(ctx context.Context)
	FastForward(ctx context.Context)
	Rewind(ctx context.Context)
	VolumeUp(ctx context.Context)
	VolumeDown(ctx context.Context)
	Mute(ctx context.Context)
	Unmute(ctx context.Context)
	Shuffle(ctx context.Context, shuffle bool)

	IsPlaying() bool
	IsShuffle() bool
	IsMuted() bool

	StartPlaylist(ctx context.Context, id string)
}

// MediaPlayerSettingController describes the functions which this screen will use to interact with the player setting source.
type MediaPlayerSettingController interface {
	GetDevices(ctx context.Context) []AudioOutput
	PlayOnDevice(ctx context.Context, deviceID string)
}

// MediaPlaylistController describes the functions which this screen will use to interact with the playlist data source.
type MediaPlaylistController interface {
	GetPlaylists(count int, offset int) []MediaPlaylist
	RefreshPlaylists(ctx context.Context) error
}
