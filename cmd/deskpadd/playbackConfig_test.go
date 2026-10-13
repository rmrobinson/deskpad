package main

import (
	"testing"

	"github.com/spf13/viper"
)

func TestLoadPlaybackSettings(t *testing.T) {
	tests := []struct {
		name       string
		values     map[string]interface{}
		want       playbackSettings
		wantErrMsg bool
	}{
		{
			name:   "nothing set uses Spotify control",
			values: map[string]interface{}{},
			want:   playbackSettings{Control: playbackControlSpotify},
		},
		{
			name:   "spotify control",
			values: map[string]interface{}{"playback.control": "spotify"},
			want:   playbackSettings{Control: playbackControlSpotify},
		},
		{
			name: "local control with connect device and prefix",
			values: map[string]interface{}{
				"playback.control":        "local",
				"playback.connect-device": "deskpad",
				"playback.mpris-prefix":   "org.mpris.MediaPlayer2.spotifyd",
			},
			want: playbackSettings{
				Control:       playbackControlLocal,
				ConnectDevice: "deskpad",
				MPRISPrefix:   "org.mpris.MediaPlayer2.spotifyd",
			},
		},
		{
			name:   "local control without a connect device",
			values: map[string]interface{}{"playback.control": "Local"},
			want:   playbackSettings{Control: playbackControlLocal},
		},
		{
			name:   "legacy use-mpris true is local control",
			values: map[string]interface{}{"use-mpris": true},
			want:   playbackSettings{Control: playbackControlLocal, Legacy: true},
		},
		{
			name:   "legacy use-mpris false is Spotify control",
			values: map[string]interface{}{"use-mpris": false},
			want:   playbackSettings{Control: playbackControlSpotify, Legacy: true},
		},
		{
			name:   "playback.control wins over use-mpris",
			values: map[string]interface{}{"playback.control": "spotify", "use-mpris": true},
			want:   playbackSettings{Control: playbackControlSpotify},
		},
		{
			name:       "unknown control mode",
			values:     map[string]interface{}{"playback.control": "mpris"},
			wantErrMsg: true,
		},
		{
			name:       "local-only keys with Spotify control",
			values:     map[string]interface{}{"playback.control": "spotify", "playback.connect-device": "deskpad"},
			wantErrMsg: true,
		},
		{
			name:       "local-only keys with legacy use-mpris false",
			values:     map[string]interface{}{"use-mpris": false, "playback.mpris-prefix": "org.mpris.MediaPlayer2.spotifyd"},
			wantErrMsg: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := viper.New()
			for k, val := range tt.values {
				v.Set(k, val)
			}

			got, err := loadPlaybackSettings(v)
			if tt.wantErrMsg {
				if err == nil {
					t.Fatalf("loadPlaybackSettings() = %+v, want an error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("loadPlaybackSettings() error: %s", err)
			}
			if got != tt.want {
				t.Errorf("loadPlaybackSettings() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
