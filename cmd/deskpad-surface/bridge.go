package main

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"sync"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
)

// keyDevice is the part of a Stream Deck the bridge uses. *sdeck.Client implements it.
type keyDevice interface {
	NumKeys() int
	FillImage(keyIdx int, img image.Image) error
	ClearKey(keyIdx int) error
	ClearAllKeys() error
	Subscribe() <-chan sdeck.Event
}

// bridge shows a deskpadd instance's control surface on a locally attached Stream Deck and sends its key presses
// back, so the daemon, and every integration it owns, can run on another machine.
type bridge struct {
	dev    keyDevice
	server string
	token  string
	client *http.Client

	// longPress is how long a key is held before it counts as a long press, matching the daemon's own Stream Deck.
	longPress time.Duration
	// idleTimeout is how long the event stream may be silent before it is considered dead.
	idleTimeout time.Duration
	// pressTimeout bounds how long a key press waits for the daemon.
	pressTimeout time.Duration
	minBackoff   time.Duration
	maxBackoff   time.Duration

	mu        sync.Mutex
	connected bool
	// painted holds what is currently on each key as the data URL it came from, "" for blank, so only keys that
	// changed are rewritten. nil means nothing is known to be on the device.
	painted []string
	downAt  map[int]time.Time
}

func newBridge(dev keyDevice, server, token string) *bridge {
	return &bridge{
		dev:          dev,
		server:       server,
		token:        token,
		client:       &http.Client{},
		longPress:    500 * time.Millisecond,
		idleTimeout:  90 * time.Second,
		pressTimeout: 3 * time.Second,
		minBackoff:   time.Second,
		maxBackoff:   30 * time.Second,
		downAt:       make(map[int]time.Time),
	}
}

// Run keeps the Stream Deck in step with the daemon until ctx is done. A daemon that is unreachable is retried
// with backoff; an error writing to the Stream Deck itself is returned, since the process can't recover from that
// and is better restarted.
func (b *bridge) Run(ctx context.Context) error {
	go b.handleKeys(ctx)

	backoff := b.minBackoff
	for ctx.Err() == nil {
		progressed, err := b.session(ctx)

		var devErr *deviceError
		if errors.As(err, &devErr) {
			return devErr
		}
		if ctx.Err() != nil {
			break
		}

		if progressed {
			backoff = b.minBackoff
		}
		log.Printf("daemon connection ended: %s; retrying in %s\n", err.Error(), backoff)

		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > b.maxBackoff {
			backoff = b.maxBackoff
		}
	}

	b.disconnected()
	return nil
}

// session makes one connection to the daemon: it paints the current state, then follows updates until the
// connection ends. progressed reports whether at least the first state was received.
func (b *bridge) session(ctx context.Context) (progressed bool, err error) {
	state, err := fetchState(ctx, b.client, b.server)
	if err != nil {
		return false, fmt.Errorf("fetch state: %w", err)
	}
	if err := b.apply(state); err != nil {
		return false, err
	}
	b.setConnected(true)
	defer b.disconnected()

	var applyErr error
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = streamStates(streamCtx, b.client, b.server, b.idleTimeout, func(s uiState) {
		if applyErr != nil {
			return
		}
		if applyErr = b.apply(s); applyErr != nil {
			cancel()
		}
	})
	if applyErr != nil {
		return true, applyErr
	}
	return true, err
}

type deviceError struct{ err error }

func (e *deviceError) Error() string { return "stream deck: " + e.err.Error() }
func (e *deviceError) Unwrap() error { return e.err }

// apply paints the keys that differ from what is already shown.
func (b *bridge) apply(state uiState) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	keyCount := b.dev.NumKeys()
	if b.painted == nil {
		b.painted = make([]string, keyCount)
		// Unknown contents: force every key to be written the first time.
		for i := range b.painted {
			b.painted[i] = "\x00unknown"
		}
	}

	for i := 0; i < keyCount; i++ {
		want := ""
		if i < len(state.Keys) && state.Keys[i] != nil {
			want = *state.Keys[i]
		}
		if b.painted[i] == want {
			continue
		}

		if err := b.paint(i, want); err != nil {
			return &deviceError{err: err}
		}
		b.painted[i] = want
	}

	return nil
}

func (b *bridge) paint(key int, dataURL string) error {
	if dataURL == "" {
		return b.dev.ClearKey(key)
	}

	img, err := decodeKeyImage(dataURL)
	if err != nil {
		// A bad image is the daemon's problem, not the device's: show a blank key and carry on.
		log.Printf("key %d: %s\n", key, err.Error())
		return b.dev.ClearKey(key)
	}

	return b.dev.FillImage(key, img)
}

func (b *bridge) setConnected(connected bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = connected
}

// disconnected blanks the keys, so a stale screen can't be pressed with nothing listening, and forgets what was shown.
func (b *bridge) disconnected() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = false
	if b.painted == nil {
		return
	}

	b.painted = nil
	if err := b.dev.ClearAllKeys(); err != nil {
		log.Printf("unable to clear keys: %s\n", err.Error())
	}
}

// handleKeys turns Stream Deck events into key presses on the daemon.
func (b *bridge) handleKeys(ctx context.Context) {
	events := b.dev.Subscribe()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}

			switch event.Type {
			case sdeck.EventTypeDown:
				b.mu.Lock()
				b.downAt[event.Key] = time.Now()
				b.mu.Unlock()

			case sdeck.EventTypeUp:
				b.mu.Lock()
				down, ok := b.downAt[event.Key]
				delete(b.downAt, event.Key)
				connected := b.connected
				b.mu.Unlock()

				if !ok {
					continue
				}

				kind := "short"
				if time.Since(down) >= b.longPress {
					kind = "long"
				}
				b.press(ctx, event.Key, kind, connected)
			}
		}
	}
}

func (b *bridge) press(ctx context.Context, key int, kind string, connected bool) {
	if !connected {
		log.Printf("key %d %s press dropped: not connected to the daemon\n", key, kind)
		return
	}

	ctx, cancel := context.WithTimeout(ctx, b.pressTimeout)
	defer cancel()

	if err := pressKey(ctx, b.client, b.server, b.token, key, kind); err != nil {
		log.Printf("key %d %s press failed: %s\n", key, kind, err.Error())
	}
}
