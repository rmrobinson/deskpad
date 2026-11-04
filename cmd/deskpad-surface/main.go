// deskpad-surface shows a deskpadd control surface on a Stream Deck attached to this machine.
//
// deskpadd normally drives a Stream Deck plugged into the machine it runs on. This program is for when the Stream
// Deck has to be somewhere else: it follows the daemon's HTTP surface API, paints the keys, and reports key presses
// back, so the daemon and all its integrations stay where they are.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
)

func main() {
	if err := run(); err != nil {
		log.Printf("%s\n", err.Error())
		os.Exit(1)
	}
}

// run is separate from main so that the deferred device close always runs, even on the error path.
func run() error {
	server := flag.String("server", os.Getenv("DESKPAD_SERVER"), "base URL of deskpadd's web address, e.g. http://h033:1338 (or DESKPAD_SERVER)")
	longPress := flag.Duration("long-press", 500*time.Millisecond, "how long a key is held to count as a long press")
	idle := flag.Duration("idle-timeout", 90*time.Second, "reconnect if the daemon is silent for this long (it sends a heartbeat every 30s)")
	flag.Parse()

	// The token is read from the environment rather than a flag so it doesn't show up in the process list.
	token := os.Getenv("DESKPAD_TOKEN")

	base, err := normalizeServer(*server)
	if err != nil {
		return fmt.Errorf("invalid -server: %w", err)
	}
	if token == "" {
		log.Printf("*** DESKPAD_TOKEN is not set; the daemon will reject key presses\n")
	} else if cleartextRemote(base) {
		log.Printf("*** %s is plain http to a non-local host: the token is sent unencrypted with every key press. Use a trusted network or https.\n", base)
	}

	// Like deskpadd, this only supports the original 15-key Stream Deck; the daemon's screens are laid out for it.
	sd, err := sdeck.New(sdeck.StreamDeckOriginalV2)
	if err != nil {
		return fmt.Errorf("unable to initialize stream deck: %w", err)
	}
	defer sd.Close()

	serial, err := sd.Serial()
	if err != nil {
		return fmt.Errorf("unable to get serial number: %w", err)
	}
	log.Printf("*** Using stream deck '%s', following %s\n", serial, base)

	if err := sd.ClearAllKeys(); err != nil {
		return fmt.Errorf("error resetting deck - consider unplugging & replugging the stream deck: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	b := newBridge(sd, base, token)
	b.longPress = *longPress
	b.idleTimeout = *idle

	// On a device error the device is probably gone, so don't try to write to it again on the way out.
	return b.Run(ctx)
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

// cleartextRemote reports whether the address is plain http to something other than this machine, where the bearer
// token would cross the network unencrypted.
func cleartextRemote(server string) bool {
	u, err := url.Parse(server)
	if err != nil || u.Scheme != "http" {
		return false
	}

	host := u.Hostname()
	if host == "localhost" {
		return false
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return false
	}

	return true
}
