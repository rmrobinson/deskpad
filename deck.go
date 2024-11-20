package deskpad

import (
	"context"
	"image"
	"log"
	"sync"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
)

// KeyPressType indicates if there was a short or a long keypress
type KeyPressType int

const (
	KeyPressShort KeyPressType = iota
	KeyPressLong
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

// Deck sits between a specific screen and the Screen Deck
type Deck struct {
	sd *sdeck.Client

	screen Screen

	lastKeyDown time.Time

	lock sync.Mutex
}

// NewDeck creates a new instance of the deck handler.
func NewDeck(sd *sdeck.Client, screen Screen) *Deck {
	d := &Deck{
		sd:     sd,
		screen: screen,
	}

	return d
}

// ChangeScreen allows for the currently displayed screen to be updated to the specified screen.
func (d *Deck) ChangeScreen(ctx context.Context, s Screen) {
	d.lock.Lock()
	defer d.lock.Unlock()

	d.screen = s
	d.RefreshScreen()
}

// RefreshScreen queries the active screen for a set of icons and displays them on the stream deck.
func (d *Deck) RefreshScreen() {
	d.sd.ClearAllKeys()

	keys := d.screen.Show()
	for keyID, keyImg := range keys {
		if keyImg == nil {
			d.sd.ClearKey(keyID)
			continue
		}
		err := d.sd.FillImage(keyID, keyImg)
		if err != nil {
			log.Printf("error filling image to key %d on screen %s: %s\n", keyID, d.screen.Name(), err.Error())
		}
	}
}

// Screen returns the currently active screen
func (d *Deck) Screen() Screen {
	return d.screen
}

// Run starts the loop of listening for inputs from the user on the stream deck.
func (d *Deck) Run(ctx context.Context) {
	d.RefreshScreen()
	events := d.sd.Subscribe()

	for {
		select {
		case <-ctx.Done():
			d.sd.ClearAllKeys()
			return

		case event := <-events:
			if event.Type == sdeck.EventTypeDown {
				d.lastKeyDown = time.Now()
				continue
			} else if event.Type == sdeck.EventTypeUp {
				t := KeyPressShort
				if d.lastKeyDown.Add(longKeypressDuration).Before(time.Now()) {
					t = KeyPressLong
				}

				keyCtx, keyCtxCancel := context.WithTimeout(ctx, keyHandlingTimeoutDuration)

				d.lock.Lock()
				action, err := d.screen.KeyPressed(keyCtx, event.Key, t)
				if err != nil {
					log.Printf("screen %s got error handling key press for key %d: %s\n", d.screen.Name(), event.Key, err.Error())
					d.lock.Unlock()
					keyCtxCancel()
					continue
				}
				d.lock.Unlock()

				switch action.Action {
				case KeyPressActionChangeScreen:
					if action.NewScreen == nil {
						log.Fatal("deck asked to update screen but provided null screen")
						keyCtxCancel()
						continue
					}
					d.ChangeScreen(keyCtx, action.NewScreen)
				case KeyPressActionUpdateIcon:
					if action.NewIcon == nil {
						log.Fatal("deck asked to update icon but provided null icon")
						keyCtxCancel()
						continue
					}

					err = d.sd.FillImage(event.Key, action.NewIcon)
					if err != nil {
						log.Printf("deck got error setting image for key %d: %s\n", event.Key, err.Error())
					}
				case KeyPressActionRefreshScreen:
					d.RefreshScreen()
				case KeyPressActionNoop:
					// Nothing to do!
				}

				keyCtxCancel()
				continue
			}

			log.Printf("received unhandled event type %d for key %d\n", event.Type, event.Key)
		}
	}
}
