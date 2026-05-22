package deskpad

import (
	"context"
	"image"
	"log"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
)

// StreamDeckSurface renders state to a physical Stream Deck and forwards key events.
type StreamDeckSurface struct {
	sd          *sdeck.Client
	lastKeyDown time.Time
}

func NewStreamDeckSurface(sd *sdeck.Client) *StreamDeckSurface {
	return &StreamDeckSurface{sd: sd}
}

func (s *StreamDeckSurface) ID() string {
	id, err := s.sd.Serial()
	if err != nil {
		log.Printf("error getting streamdeck id: %s\n", err.Error())
		return ""
	}

	return id
}

func (s *StreamDeckSurface) KeyCount() int {
	return s.sd.NumKeys()
}

func (s *StreamDeckSurface) Refresh(snapshot Snapshot) error {
	if err := s.sd.ClearAllKeys(); err != nil {
		return err
	}

	for keyID, keyImg := range snapshot.Keys {
		if err := s.fillKey(keyID, keyImg); err != nil {
			return err
		}
	}

	return nil
}

func (s *StreamDeckSurface) UpdateKey(snapshot Snapshot, keyID int) error {
	if keyID < 0 || keyID >= len(snapshot.Keys) {
		return nil
	}

	return s.fillKey(keyID, snapshot.Keys[keyID])
}

func (s *StreamDeckSurface) Clear() error {
	return s.sd.ClearAllKeys()
}

// Run starts the loop of listening for inputs from the physical Stream Deck.
func (s *StreamDeckSurface) Run(ctx context.Context, d *Deck) {
	events := s.sd.Subscribe()

	for {
		select {
		case <-ctx.Done():
			if err := s.Clear(); err != nil {
				log.Printf("error clearing surface %s: %s\n", s.ID(), err.Error())
			}
			return

		case event := <-events:
			if event.Type == sdeck.EventTypeDown {
				s.lastKeyDown = time.Now()
				continue
			} else if event.Type == sdeck.EventTypeUp {
				t := KeyPressShort
				if s.lastKeyDown.Add(longKeypressDuration).Before(time.Now()) {
					t = KeyPressLong
				}

				_ = d.PressKey(ctx, event.Key, t)
				continue
			}

			log.Printf("received unhandled event type %d for key %d\n", event.Type, event.Key)
		}
	}
}

func (s *StreamDeckSurface) fillKey(keyID int, keyImg image.Image) error {
	if keyImg == nil {
		return s.sd.ClearKey(keyID)
	}

	return s.sd.FillImage(keyID, keyImg)
}
