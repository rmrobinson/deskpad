package main

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

// playbackControl is how media playback is controlled. Spotify is involved either way; the difference is whether
// Spotify does everything or only starts the media.
type playbackControl string

const (
	// playbackControlSpotify controls playback entirely through the Spotify Web API. No local player is needed, so
	// this works on any OS, but the controlled device is whichever one Spotify considers active.
	playbackControlSpotify playbackControl = "spotify"
	// playbackControlLocal uses Spotify only to launch media on a Spotify Connect device (optional), and controls
	// playback, state and volume locally over MPRIS and PulseAudio. Linux only.
	playbackControlLocal playbackControl = "local"
)

type playbackSettings struct {
	Control playbackControl
	// ConnectDevice is the Spotify Connect device playlists are launched on. Local control only. If empty the local
	// MPRIS player is asked to open the playlist itself.
	ConnectDevice string
	// MPRISPrefix limits local control to MPRIS players whose D-Bus name starts with it. Local control only.
	MPRISPrefix string
	// Legacy is set when the control mode was taken from the deprecated use-mpris key.
	Legacy bool
}

// loadPlaybackSettings reads the playback.* keys. The deprecated use-mpris bool is honoured when playback.control
// isn't set: true means local control and false means Spotify control.
func loadPlaybackSettings(v *viper.Viper) (playbackSettings, error) {
	s := playbackSettings{
		ConnectDevice: v.GetString("playback.connect-device"),
		MPRISPrefix:   v.GetString("playback.mpris-prefix"),
	}

	switch {
	case v.IsSet("playback.control"):
		control := playbackControl(strings.ToLower(strings.TrimSpace(v.GetString("playback.control"))))
		if control != playbackControlSpotify && control != playbackControlLocal {
			return s, fmt.Errorf("playback.control must be %q or %q, not %q", playbackControlSpotify, playbackControlLocal, v.GetString("playback.control"))
		}
		s.Control = control
	case v.IsSet("use-mpris"):
		s.Legacy = true
		if v.GetBool("use-mpris") {
			s.Control = playbackControlLocal
		} else {
			s.Control = playbackControlSpotify
		}
	default:
		s.Control = playbackControlSpotify
	}

	if s.Control == playbackControlSpotify && (s.ConnectDevice != "" || s.MPRISPrefix != "") {
		return s, fmt.Errorf("playback.connect-device and playback.mpris-prefix only apply when playback.control is %q", playbackControlLocal)
	}

	return s, nil
}
