package deskpad

import (
	"context"
	"fmt"
	"image"
	"log"
	"sync"
	"time"
)

// KeyPressType indicates if there was a short or a long keypress
type KeyPressType int

const (
	KeyPressShort KeyPressType = iota
	KeyPressLong
)

const (
	defaultKeyCount = 15
)

var (
	longKeypressDuration, _       = time.ParseDuration("500ms")
	keyHandlingTimeoutDuration, _ = time.ParseDuration("2s")
)

// KeyPressActionType indicates the type of action which could be taken when processing the key press
type KeyPressActionType int

const (
	KeyPressActionChangeScreen = iota
	KeyPressActionUpdateIcon
	KeyPressActionRefreshScreen
	KeyPressActionNoop
)

// KeyPressAction contains the information necessary to handle the result of a key press
type KeyPressAction struct {
	Action KeyPressActionType

	NewScreen Screen
	NewIcon   image.Image
}

// Deck coordinates a screen across all registered control surfaces.
type Deck struct {
	screen   Screen
	surfaces []Surface
	keys     []image.Image
	rows     int
	columns  int

	lock      sync.RWMutex
	pressLock sync.Mutex
}

// NewDeck creates a new instance of the deck handler.
func NewDeck(screen Screen) *Deck {
	rows, columns := deckGeometry(defaultKeyCount)

	return &Deck{
		screen:  screen,
		keys:    make([]image.Image, defaultKeyCount),
		rows:    rows,
		columns: columns,
	}
}

// RegisterSurface adds a control surface which should receive future screen renders.
func (d *Deck) RegisterSurface(s Surface) {
	if s == nil {
		return
	}

	d.lock.Lock()
	resized := d.configureGeometryLocked(s.KeyCount())
	d.surfaces = append(d.surfaces, s)
	snapshot := d.snapshotLocked()
	screen := d.screen
	d.lock.Unlock()

	if resized {
		d.renderScreen(screen)
		return
	}

	if err := s.Refresh(snapshot); err != nil {
		log.Printf("error refreshing surface %s: %s\n", s.ID(), err.Error())
	}
}

// ChangeScreen allows for the currently displayed screen to be updated to the specified screen.
func (d *Deck) ChangeScreen(ctx context.Context, s Screen) {
	d.renderScreen(s)
}

// RefreshScreen queries the active screen for a set of icons and displays them on the control surfaces.
func (d *Deck) RefreshScreen() {
	d.lock.RLock()
	screen := d.screen
	d.lock.RUnlock()

	d.renderScreen(screen)
}

// Screen returns the currently active screen
func (d *Deck) Screen() Screen {
	d.lock.RLock()
	defer d.lock.RUnlock()

	return d.screen
}

// ID returns the ID of the first registered control surface.
func (d *Deck) ID() string {
	d.lock.RLock()
	defer d.lock.RUnlock()

	if len(d.surfaces) == 0 {
		return ""
	}

	return d.surfaces[0].ID()
}

// KeyCount returns the number of keys on the mirrored control surface.
func (d *Deck) KeyCount() int {
	d.lock.RLock()
	defer d.lock.RUnlock()

	return len(d.keys)
}

// Snapshot returns the current rendered control-surface state.
func (d *Deck) Snapshot() Snapshot {
	d.lock.RLock()
	defer d.lock.RUnlock()

	return d.snapshotLocked()
}

// Clear clears all registered control surfaces.
func (d *Deck) Clear() {
	d.lock.RLock()
	surfaces := append([]Surface(nil), d.surfaces...)
	d.lock.RUnlock()

	for _, surface := range surfaces {
		if err := surface.Clear(); err != nil {
			log.Printf("error clearing surface %s: %s\n", surface.ID(), err.Error())
		}
	}
}

// PressKey handles a key press from any control surface.
func (d *Deck) PressKey(ctx context.Context, keyID int, t KeyPressType) error {
	keyCtx, keyCtxCancel := context.WithTimeout(ctx, keyHandlingTimeoutDuration)
	defer keyCtxCancel()

	d.pressLock.Lock()
	defer d.pressLock.Unlock()

	d.lock.RLock()
	if keyID < 0 || keyID >= len(d.keys) {
		d.lock.RUnlock()
		return fmt.Errorf("invalid key id %d", keyID)
	}
	screen := d.screen
	screenName := screen.Name()
	d.lock.RUnlock()

	action, err := screen.KeyPressed(keyCtx, keyID, t)
	if err != nil {
		log.Printf("screen %s got error handling key press for key %d: %s\n", screenName, keyID, err.Error())
		return err
	}

	switch action.Action {
	case KeyPressActionChangeScreen:
		if action.NewScreen == nil {
			log.Fatal("deck asked to update screen but provided null screen")
			return nil
		}
		d.renderScreen(action.NewScreen)
	case KeyPressActionUpdateIcon:
		if action.NewIcon == nil {
			log.Fatal("deck asked to update icon but provided null icon")
			return nil
		}

		d.lock.Lock()
		if keyID < 0 || keyID >= len(d.keys) {
			d.lock.Unlock()
			return fmt.Errorf("invalid key id %d", keyID)
		}
		d.keys[keyID] = action.NewIcon
		snapshot := d.snapshotLocked()
		surfaces := d.surfacesLocked()
		d.lock.Unlock()

		d.updateKey(surfaces, snapshot, keyID)
	case KeyPressActionRefreshScreen:
		d.renderScreen(screen)
	case KeyPressActionNoop:
		// Nothing to do!
	}

	return nil
}

func (d *Deck) renderScreen(screen Screen) {
	if screen == nil {
		return
	}

	d.lock.RLock()
	keyCount := len(d.keys)
	d.lock.RUnlock()

	renderedKeys := make([]image.Image, keyCount)
	keys := screen.Show()
	copy(renderedKeys, keys)

	d.lock.Lock()
	d.screen = screen
	d.keys = renderedKeys
	snapshot := d.snapshotLocked()
	surfaces := d.surfacesLocked()
	d.lock.Unlock()

	d.refreshSurfaces(surfaces, snapshot)
}

func (d *Deck) refreshSurfaces(surfaces []Surface, snapshot Snapshot) {
	for _, surface := range surfaces {
		if err := surface.Refresh(snapshot); err != nil {
			log.Printf("error refreshing surface %s for screen %s: %s\n", surface.ID(), snapshot.ScreenName, err.Error())
		}
	}
}

func (d *Deck) updateKey(surfaces []Surface, snapshot Snapshot, keyID int) {
	for _, surface := range surfaces {
		if err := surface.UpdateKey(snapshot, keyID); err != nil {
			log.Printf("deck got error setting image for key %d on surface %s: %s\n", keyID, surface.ID(), err.Error())
		}
	}
}

func (d *Deck) surfacesLocked() []Surface {
	return append([]Surface(nil), d.surfaces...)
}

func (d *Deck) snapshotLocked() Snapshot {
	keys := make([]image.Image, len(d.keys))
	copy(keys, d.keys)

	return Snapshot{
		ScreenName: d.screen.Name(),
		Rows:       d.rows,
		Columns:    d.columns,
		Keys:       keys,
	}
}

func (d *Deck) configureGeometryLocked(keyCount int) bool {
	if keyCount <= 0 || keyCount == len(d.keys) {
		return false
	}

	rows, columns := deckGeometry(keyCount)
	d.keys = make([]image.Image, keyCount)
	d.rows = rows
	d.columns = columns
	return true
}

func deckGeometry(keyCount int) (int, int) {
	if keyCount <= 0 {
		keyCount = defaultKeyCount
	}

	rows := 3
	if keyCount <= 6 {
		rows = 2
	}

	columns := (keyCount + rows - 1) / rows
	if columns == 0 {
		columns = 1
	}

	return rows, columns
}
