// deskpad-surface shows a deskpadd control surface on a Stream Deck attached to this machine.
//
// deskpadd normally drives a Stream Deck plugged into the machine it runs on. This program is for when the Stream
// Deck has to be somewhere else: it follows the daemon's HTTP surface API, paints the keys, and reports key presses
// back, so the daemon and all its integrations stay where they are.
package main

import (
	"context"
	"flag"
	"log"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
)

func main() {
	server := flag.String("server", os.Getenv("DESKPAD_SERVER"), "base URL of deskpadd's web address, e.g. http://h033:1338 (or DESKPAD_SERVER)")
	longPress := flag.Duration("long-press", 500*time.Millisecond, "how long a key is held to count as a long press")
	idle := flag.Duration("idle-timeout", 90*time.Second, "reconnect if the daemon is silent for this long (it sends a heartbeat every 30s)")
	flag.Parse()

	// The token is read from the environment rather than a flag so it doesn't show up in the process list.
	token := os.Getenv("DESKPAD_TOKEN")

	base, err := normalizeServer(*server)
	if err != nil {
		log.Fatalf("invalid -server: %s\n", err.Error())
	}
	if token == "" {
		log.Printf("*** DESKPAD_TOKEN is not set; the daemon will reject key presses\n")
	}

	// Like deskpadd, this only supports the original 15-key Stream Deck; the daemon's screens are laid out for it.
	sd, err := sdeck.New(sdeck.StreamDeckOriginalV2)
	if err != nil {
		log.Fatalf("unable to initialize stream deck: %s\n", err.Error())
	}
	defer sd.Close()

	serial, err := sd.Serial()
	if err != nil {
		log.Fatalf("unable to get serial number: %s\n", err.Error())
	}
	log.Printf("*** Using stream deck '%s', following %s\n", serial, base)

	if err := sd.ClearAllKeys(); err != nil {
		log.Fatalf("error resetting deck - consider unplugging & replugging the stream deck. Details: %s\n", err.Error())
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	b := newBridge(sd, base, token)
	b.longPress = *longPress
	b.idleTimeout = *idle

	if err := b.Run(ctx); err != nil {
		_ = sd.ClearAllKeys()
		log.Fatalf("%s\n", err.Error())
	}
	_ = sd.ClearAllKeys()
}

// normalizeServer checks the daemon address and strips any trailing slash.
func normalizeServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errMissingServer
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", errBadServer
	}

	return strings.TrimRight(u.String(), "/"), nil
}
