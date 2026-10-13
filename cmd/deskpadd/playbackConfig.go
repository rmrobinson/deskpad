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
	// Ignored lists configured keys that don't apply to the control mode and were dropped.
	Ignored []string
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

	// These only mean something for local control. Leaving them in place while switching to Spotify control is
	// harmless, so drop them and say so rather than refusing to start.
	if s.Control == playbackControlSpotify {
		if s.ConnectDevice != "" {
			s.Ignored = append(s.Ignored, "playback.connect-device")
			s.ConnectDevice = ""
		}
		if s.MPRISPrefix != "" {
			s.Ignored = append(s.Ignored, "playback.mpris-prefix")
			s.MPRISPrefix = ""
		}
	}

	return s, nil
}
