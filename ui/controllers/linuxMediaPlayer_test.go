package controllers

import (
	"testing"

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
