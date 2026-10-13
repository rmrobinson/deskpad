package controllers

import (
	"testing"

	"github.com/godbus/dbus"
	"github.com/zmb3/spotify/v2"
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

func TestChoosePlayer(t *testing.T) {
	tests := []struct {
		name    string
		names   []string
		current string
		want    string
	}{
		{"nothing on the bus", nil, "", ""},
		{"current has gone and nothing else", nil, "a", ""},
		{"first player found", []string{"a", "b"}, "", "a"},
		{"keeps current while present", []string{"a", "b"}, "b", "b"},
		{"replaces a vanished current", []string{"b"}, "a", "b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := choosePlayer(tt.names, tt.current); got != tt.want {
				t.Errorf("choosePlayer(%v, %q) = %q, want %q", tt.names, tt.current, got, tt.want)
			}
		})
	}
}

func TestFilterMPRISNames(t *testing.T) {
	names := []string{
		"org.mpris.MediaPlayer2.ncspot.instance1",
		"org.mpris.MediaPlayer2.spotifyd.instance2",
		"org.mpris.MediaPlayer2.firefox.instance3",
	}

	if got := filterMPRISNames(names, ""); len(got) != 3 {
		t.Errorf("empty prefix returned %v, want all names", got)
	}

	got := filterMPRISNames(names, "org.mpris.MediaPlayer2.spotifyd")
	if len(got) != 1 || got[0] != "org.mpris.MediaPlayer2.spotifyd.instance2" {
		t.Errorf("spotifyd prefix returned %v", got)
	}

	if got := filterMPRISNames(names, "org.mpris.MediaPlayer2.vlc"); len(got) != 0 {
		t.Errorf("non-matching prefix returned %v, want none", got)
	}
}

func TestFindConnectDevice(t *testing.T) {
	devices := []spotify.PlayerDevice{
		{ID: "mac-id", Name: "Robert’s MacBook Pro"},
		{ID: "deskpad-id", Name: "deskpad"},
	}

	if id, ok := findConnectDevice(devices, "DeskPad"); !ok || id != "deskpad-id" {
		t.Errorf("findConnectDevice(DeskPad) = (%q, %t), want (deskpad-id, true)", id, ok)
	}
	if _, ok := findConnectDevice(devices, "kitchen"); ok {
		t.Errorf("findConnectDevice(kitchen) found a device, want none")
	}
	if _, ok := findConnectDevice(nil, "deskpad"); ok {
		t.Errorf("findConnectDevice on no devices found a device")
	}
}
