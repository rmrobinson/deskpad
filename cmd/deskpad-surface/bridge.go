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
	// requestTimeout bounds the initial state request and each key press.
	requestTimeout time.Duration
	// blankAfter is how long the last screen stays on the keys after the daemon is lost before they go blank. Presses
	// are not sent in that time, so a brief blip doesn't flash the whole deck dark.
	blankAfter time.Duration
	// stableAfter is how long a connection has to last before the retry backoff starts over.
	stableAfter time.Duration
	minBackoff  time.Duration
	maxBackoff  time.Duration

	mu        sync.Mutex
	connected bool
	// painted holds what is currently on each key as the data URL it came from, "" for blank, so only keys that
	// changed are rewritten. nil means nothing is known to be on the device.
	painted []string
	downAt  map[int]time.Time
	blank   *time.Timer

	presses chan pressRequest

	fatalOnce sync.Once
	fatalErr  error
	cancel    context.CancelFunc
}

type pressRequest struct {
	key  int
	kind string
}

func newBridge(dev keyDevice, server, token string) *bridge {
	return &bridge{
		dev:            dev,
		server:         server,
		token:          token,
		client:         &http.Client{},
		longPress:      500 * time.Millisecond,
		idleTimeout:    90 * time.Second,
		requestTimeout: 5 * time.Second,
		blankAfter:     10 * time.Second,
		stableAfter:    10 * time.Second,
		minBackoff:     time.Second,
		maxBackoff:     30 * time.Second,
		downAt:         make(map[int]time.Time),
		presses:        make(chan pressRequest, 16),
	}
}

// Run keeps the Stream Deck in step with the daemon until ctx is done. A daemon that is unreachable is retried
// with backoff. A failure of the Stream Deck itself, whether a write error or the device going away, is returned,
// since the process can't recover from that and is better restarted.
func (b *bridge) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	b.cancel = cancel

	go b.handleKeys(ctx)
	go b.sendPresses(ctx)

	backoff := b.minBackoff
	for ctx.Err() == nil {
		started := time.Now()
		err := b.session(ctx)
		b.lost()

		if ctx.Err() != nil {
			break
		}

		// Only a connection that held for a while counts as the daemon being back; one that dies straight after
		// the first state would otherwise be retried every second forever.
		if time.Since(started) >= b.stableAfter {
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

	if b.fatalErr != nil {
		return b.fatalErr
	}

	b.blankNow()
	return nil
}

// fail records an unrecoverable Stream Deck failure and stops Run.
func (b *bridge) fail(err error) {
	b.fatalOnce.Do(func() {
		b.fatalErr = &deviceError{err: err}
		if b.cancel != nil {
			b.cancel()
		}
	})
}

// session makes one connection to the daemon: it paints the current state, then follows updates until the
// connection ends.
func (b *bridge) session(ctx context.Context) error {
	state, err := fetchState(ctx, b.client, b.server, b.requestTimeout)
	if err != nil {
		return fmt.Errorf("fetch state: %w", err)
	}
	if err := b.apply(state); err != nil {
		b.fail(errors.Unwrap(err))
		return err
	}
	b.setConnected(true)

	var applyErr error
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	err = streamStates(streamCtx, b.client, b.server, b.idleTimeout, func(s uiState) {
		if applyErr != nil {
			return
		}
		if applyErr = b.apply(s); applyErr != nil {
			b.fail(errors.Unwrap(applyErr))
			cancel()
		}
	})
	if applyErr != nil {
		return applyErr
	}
	return err
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

	if len(state.Keys) != keyCount {
		log.Printf("daemon surface has %d keys but the stream deck has %d; extra keys are ignored\n", len(state.Keys), keyCount)
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
	if connected && b.blank != nil {
		b.blank.Stop()
		b.blank = nil
	}
}

// lost handles the daemon going away. Key presses stop being sent at once, but the last screen stays visible for
// blankAfter so a brief network blip doesn't flash the deck dark; if the daemon is still gone then, the keys blank.
func (b *bridge) lost() {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.connected = false
	if b.blank != nil {
		b.blank.Stop()
	}
	b.blank = time.AfterFunc(b.blankAfter, func() {
		b.mu.Lock()
		defer b.mu.Unlock()

		if !b.connected {
			b.blankLocked()
		}
	})
}

// blankNow clears the keys and forgets what was shown, so the next connection repaints everything.
func (b *bridge) blankNow() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.blank != nil {
		b.blank.Stop()
		b.blank = nil
	}
	b.blankLocked()
}

func (b *bridge) blankLocked() {
	if b.painted == nil {
		return
	}

	b.painted = nil
	if err := b.dev.ClearAllKeys(); err != nil {
		log.Printf("unable to clear keys: %s\n", err.Error())
	}
}

// handleKeys turns Stream Deck events into queued key presses. It never waits on the daemon, so a slow request
// can't make later events look as if they happened late, which would turn taps into long presses.
func (b *bridge) handleKeys(ctx context.Context) {
	events := b.dev.Subscribe()

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-events:
			if !ok {
				b.fail(errors.New("event stream closed; the device was probably unplugged"))
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

				if !connected {
					log.Printf("key %d %s press dropped: not connected to the daemon\n", event.Key, kind)
					continue
				}

				select {
				case b.presses <- pressRequest{key: event.Key, kind: kind}:
				default:
					log.Printf("key %d %s press dropped: daemon is not keeping up\n", event.Key, kind)
				}
			}
		}
	}
}

// sendPresses delivers queued presses to the daemon in order.
func (b *bridge) sendPresses(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case p := <-b.presses:
			reqCtx, cancel := context.WithTimeout(ctx, b.requestTimeout)
			err := pressKey(reqCtx, b.client, b.server, b.token, p.key, p.kind)
			cancel()

			if err != nil {
				log.Printf("key %d %s press failed: %s\n", p.key, p.kind, err.Error())
			}
		}
	}
}
