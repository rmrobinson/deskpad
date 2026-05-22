package deskpad

import (
	"context"
	"image"
	"image/color"
	"testing"
)

type fakeScreen struct {
	name        string
	showKeys    []image.Image
	showCount   int
	pressedKey  int
	pressedType KeyPressType
	action      KeyPressAction
}

func (s *fakeScreen) Name() string {
	return s.name
}

func (s *fakeScreen) Show() []image.Image {
	s.showCount++
	return s.showKeys
}

func (s *fakeScreen) Icon() image.Image {
	return testImage(color.RGBA{R: 255, A: 255})
}

func (s *fakeScreen) KeyPressed(ctx context.Context, id int, t KeyPressType) (KeyPressAction, error) {
	s.pressedKey = id
	s.pressedType = t
	return s.action, nil
}

type fakeSurface struct {
	id            string
	refreshes     int
	updates       []int
	clears        int
	lastRefreshed []image.Image
	lastUpdated   image.Image
}

func (s *fakeSurface) ID() string {
	return s.id
}

func (s *fakeSurface) KeyCount() int {
	return 0
}

func (s *fakeSurface) Refresh(snapshot Snapshot) error {
	s.refreshes++
	s.lastRefreshed = append([]image.Image(nil), snapshot.Keys...)
	return nil
}

func (s *fakeSurface) UpdateKey(snapshot Snapshot, id int) error {
	s.updates = append(s.updates, id)
	s.lastUpdated = snapshot.Keys[id]
	return nil
}

func (s *fakeSurface) Clear() error {
	s.clears++
	return nil
}

func TestPressKeyUpdateIconUpdatesAllSurfacesAndSnapshot(t *testing.T) {
	icon := testImage(color.RGBA{G: 255, A: 255})
	screen := &fakeScreen{
		name:   "home",
		action: KeyPressAction{Action: KeyPressActionUpdateIcon, NewIcon: icon},
	}
	deck := NewDeck(screen)
	surfaceA := &fakeSurface{id: "a"}
	surfaceB := &fakeSurface{id: "b"}
	deck.RegisterSurface(surfaceA)
	deck.RegisterSurface(surfaceB)

	if err := deck.PressKey(context.Background(), 4, KeyPressShort); err != nil {
		t.Fatalf("PressKey returned error: %s", err)
	}

	if screen.pressedKey != 4 {
		t.Fatalf("pressed key = %d, want 4", screen.pressedKey)
	}
	if screen.pressedType != KeyPressShort {
		t.Fatalf("pressed type = %d, want short", screen.pressedType)
	}
	if len(surfaceA.updates) != 1 || surfaceA.updates[0] != 4 {
		t.Fatalf("surface A updates = %v, want [4]", surfaceA.updates)
	}
	if len(surfaceB.updates) != 1 || surfaceB.updates[0] != 4 {
		t.Fatalf("surface B updates = %v, want [4]", surfaceB.updates)
	}
	if got := deck.Snapshot().Keys[4]; got != icon {
		t.Fatalf("snapshot key 4 was not updated")
	}
}

func TestPressKeyChangeScreenRefreshesAllSurfaces(t *testing.T) {
	nextIcon := testImage(color.RGBA{B: 255, A: 255})
	next := &fakeScreen{name: "next", showKeys: []image.Image{nextIcon}}
	screen := &fakeScreen{
		name:   "home",
		action: KeyPressAction{Action: KeyPressActionChangeScreen, NewScreen: next},
	}
	deck := NewDeck(screen)
	surface := &fakeSurface{id: "surface"}
	deck.RegisterSurface(surface)

	if err := deck.PressKey(context.Background(), 2, KeyPressLong); err != nil {
		t.Fatalf("PressKey returned error: %s", err)
	}

	if deck.Screen().Name() != "next" {
		t.Fatalf("screen = %q, want next", deck.Screen().Name())
	}
	if next.showCount != 1 {
		t.Fatalf("next Show count = %d, want 1", next.showCount)
	}
	if surface.refreshes != 2 {
		t.Fatalf("surface refreshes = %d, want 2 including registration", surface.refreshes)
	}
	if surface.lastRefreshed[0] != nextIcon {
		t.Fatalf("surface did not receive next screen icon")
	}
	if screen.pressedType != KeyPressLong {
		t.Fatalf("pressed type = %d, want long", screen.pressedType)
	}
}

func TestPressKeyRefreshActionCallsShowAndRedrawsAllKeys(t *testing.T) {
	icon := testImage(color.RGBA{R: 100, A: 255})
	screen := &fakeScreen{
		name:     "home",
		showKeys: []image.Image{icon},
		action:   KeyPressAction{Action: KeyPressActionRefreshScreen},
	}
	deck := NewDeck(screen)
	surface := &fakeSurface{id: "surface"}
	deck.RegisterSurface(surface)

	if err := deck.PressKey(context.Background(), 1, KeyPressShort); err != nil {
		t.Fatalf("PressKey returned error: %s", err)
	}

	if screen.showCount != 1 {
		t.Fatalf("Show count = %d, want 1", screen.showCount)
	}
	if surface.lastRefreshed[0] != icon {
		t.Fatalf("surface did not receive refreshed icon")
	}
	if got := deck.Snapshot().Keys[0]; got != icon {
		t.Fatalf("snapshot key 0 was not refreshed")
	}
}

func TestDeckGeometryUsesStreamDeckKeyCount(t *testing.T) {
	tests := []struct {
		name        string
		keyCount    int
		wantRows    int
		wantColumns int
	}{
		{name: "original", keyCount: 15, wantRows: 3, wantColumns: 5},
		{name: "six key", keyCount: 6, wantRows: 2, wantColumns: 3},
		{name: "four key", keyCount: 4, wantRows: 2, wantColumns: 2},
		{name: "fallback", keyCount: 0, wantRows: 3, wantColumns: 5},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, columns := deckGeometry(tc.keyCount)
			if rows != tc.wantRows || columns != tc.wantColumns {
				t.Fatalf("geometry = %dx%d, want %dx%d", rows, columns, tc.wantRows, tc.wantColumns)
			}
		})
	}
}

func testImage(c color.Color) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, c)
	return img
}
