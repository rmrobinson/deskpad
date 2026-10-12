package controllers

import (
	"testing"
	"time"

	"github.com/godbus/dbus"
)

func TestMetadataStringHandlesMissingAndWrongTypeValues(t *testing.T) {
	metadata := map[string]dbus.Variant{
		"xesam:title": dbus.MakeVariant("Song Title"),
		"xesam:album": dbus.MakeVariant(123),
	}

	if got := metadataString(metadata, "xesam:title"); got != "Song Title" {
		t.Fatalf("title = %q, want Song Title", got)
	}
	if got := metadataString(metadata, "xesam:artist"); got != "" {
		t.Fatalf("missing artist = %q, want empty string", got)
	}
	if got := metadataString(metadata, "xesam:album"); got != "" {
		t.Fatalf("wrong type album = %q, want empty string", got)
	}
}

func TestMetadataStringsHandlesCommonMPRISArtistShapes(t *testing.T) {
	metadata := map[string]dbus.Variant{
		"stringSlice": dbus.MakeVariant([]string{"Artist One", "Artist Two"}),
		"string":      dbus.MakeVariant("Artist One"),
		"mixedSlice":  dbus.MakeVariant([]interface{}{"Artist One", 123, "Artist Two"}),
		"wrongType":   dbus.MakeVariant(123),
	}

	if got := metadataStrings(metadata, "stringSlice"); len(got) != 2 || got[0] != "Artist One" || got[1] != "Artist Two" {
		t.Fatalf("string slice artists = %v, want two artists", got)
	}
	if got := metadataStrings(metadata, "string"); len(got) != 1 || got[0] != "Artist One" {
		t.Fatalf("string artist = %v, want one artist", got)
	}
	if got := metadataStrings(metadata, "mixedSlice"); len(got) != 2 || got[0] != "Artist One" || got[1] != "Artist Two" {
		t.Fatalf("mixed slice artists = %v, want string values only", got)
	}
	if got := metadataStrings(metadata, "missing"); got != nil {
		t.Fatalf("missing artists = %v, want nil", got)
	}
	if got := metadataStrings(metadata, "wrongType"); got != nil {
		t.Fatalf("wrong type artists = %v, want nil", got)
	}
}

func TestChooseMPRISPlayer(t *testing.T) {
	flagged := func(names ...string) map[string]time.Time {
		m := make(map[string]time.Time)
		for _, n := range names {
			m[n] = time.Now()
		}
		return m
	}

	tests := []struct {
		name         string
		names        []string
		current      string
		unresponsive map[string]time.Time
		demote       bool
		want         string
	}{
		{"none available", nil, "", nil, true, ""},
		{"none available, had current", nil, "a", nil, true, ""},
		{"first player found", []string{"a", "b"}, "", nil, true, "a"},
		{"keeps responsive current", []string{"a", "b"}, "b", nil, true, "b"},
		{"replaces vanished current", []string{"b"}, "a", nil, true, "b"},
		{"demotes unresponsive current", []string{"a", "b"}, "a", flagged("a"), true, "b"},
		{"reads keep unresponsive current", []string{"a", "b"}, "a", flagged("a"), false, "a"},
		{"reads still replace vanished current", []string{"b", "c"}, "a", flagged("b"), false, "c"},
		{"skips unresponsive candidate", []string{"a", "b", "c"}, "", flagged("a"), true, "b"},
		{"keeps only-unresponsive current", []string{"a"}, "a", flagged("a"), true, "a"},
		{"all unresponsive, current gone", []string{"b", "c"}, "a", flagged("b", "c"), true, "b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := chooseMPRISPlayer(tt.names, tt.current, tt.unresponsive, tt.demote); got != tt.want {
				t.Errorf("chooseMPRISPlayer(%v, %q, %v, %t) = %q, want %q", tt.names, tt.current, tt.unresponsive, tt.demote, got, tt.want)
			}
		})
	}
}

func TestPlaybackStatusFromSignal(t *testing.T) {
	changed := func(kv map[string]interface{}) map[string]dbus.Variant {
		out := make(map[string]dbus.Variant)
		for k, v := range kv {
			out[k] = dbus.MakeVariant(v)
		}
		return out
	}

	tests := []struct {
		name       string
		signalName string
		body       []interface{}
		want       string
		wantOK     bool
	}{
		{"playing", dbusPropertiesChanged, []interface{}{mprisPlayerInterface, changed(map[string]interface{}{"PlaybackStatus": "Playing"}), []string{}}, "Playing", true},
		{"other signal", "org.freedesktop.DBus.NameOwnerChanged", []interface{}{mprisPlayerInterface, changed(nil)}, "", false},
		{"other interface", dbusPropertiesChanged, []interface{}{"org.mpris.MediaPlayer2", changed(map[string]interface{}{"PlaybackStatus": "Playing"})}, "", false},
		{"no status change", dbusPropertiesChanged, []interface{}{mprisPlayerInterface, changed(map[string]interface{}{"Volume": 0.5})}, "", false},
		{"short body", dbusPropertiesChanged, []interface{}{mprisPlayerInterface}, "", false},
		{"wrong status type", dbusPropertiesChanged, []interface{}{mprisPlayerInterface, changed(map[string]interface{}{"PlaybackStatus": 3})}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := playbackStatusFromSignal(tt.signalName, tt.body)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("playbackStatusFromSignal() = (%q, %t), want (%q, %t)", got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
