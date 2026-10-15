# Remote Stream Deck surface

`deskpad-surface` shows a deskpadd control surface on a Stream Deck attached to **another machine**, typically a
small Raspberry Pi, and sends the key presses back. The daemon, and everything it owns (MPRIS, PulseAudio, the
Timebox, Spotify), stays where it is.

```
Stream Deck ── USB ── Pi: deskpad-surface ── HTTP ── host: deskpadd
                          paints keys  ◄── GET /api/ui/events (images)
                          presses      ──► POST /api/ui/keys/{id}/press
```

It uses only the daemon's existing web API, the same one the browser UI uses, so the daemon needs no changes
beyond `web.addr` being reachable and `web.auth-token` being set.

## Daemon side

On the machine running deskpadd:

- `web.addr` must be reachable from the Pi (the default `:1337` listens on all interfaces; check any firewall).
- `web.auth-token` must be set, or key presses are refused.
- If the Stream Deck has moved off that machine, set `use-streamdeck: false` there. Leave it `true` to have both
  working; they are independent surfaces of the same screens.

The traffic is plain HTTP and the token travels in a header, so keep it on a trusted LAN or put TLS in front
(`DESKPAD_SERVER` accepts `https://`). The program logs a warning at startup when it will send the token over
plain `http://` to anything other than this machine.

## Build (on the Pi)

The Stream Deck's HID library is CGO and needs `libudev` headers for the machine it runs on, so build there
rather than cross-compiling:

```sh
sudo apt install golang-go libudev-dev git
git clone https://github.com/rmrobinson/deskpad && cd deskpad
make build-surface          # bin/deskpad-surface
```

The Go toolchain must be new enough to read this module's `go.mod`. A distribution Go that is 1.21 or later will
download the exact toolchain it asks for; an older one (Raspberry Pi OS based on Debian 12 ships 1.19) cannot, so
use a newer distribution or install Go from [go.dev](https://go.dev/dl/).

## Install (on the Pi)

```sh
# 1. Let your user open the Stream Deck, and log back in so the group applies.
sudo cp deploy/surface/99-streamdeck.rules /etc/udev/rules.d/
sudo udevadm control --reload && sudo udevadm trigger
sudo usermod -aG plugdev "$USER"

# 2. Binary, config and unit.
install -Dm0755 bin/deskpad-surface ~/.local/bin/deskpad-surface
install -Dm0600 deploy/surface/surface.env.example ~/.deskpad/surface.env
$EDITOR ~/.deskpad/surface.env
install -Dm0644 deploy/surface/deskpad-surface.service ~/.config/systemd/user/deskpad-surface.service

# 3. Start it, and keep it running without a login session.
systemctl --user daemon-reload
systemctl --user enable --now deskpad-surface
sudo loginctl enable-linger "$USER"
```

Check `journalctl --user -u deskpad-surface -f`. You should see the Stream Deck's serial number and the daemon
address, and the keys should fill in within a second or so.

## Behaviour

- **Painting.** On connect it reads the current screen and then follows the event stream, rewriting only the keys
  whose image changed.
- **Presses.** A key held for 500ms or longer is a long press, matching the daemon's own Stream Deck (`-long-press`
  changes it). Presses made while the daemon is unreachable are dropped, not queued.
- **Daemon unavailable.** Presses stop being sent at once and it retries with backoff, up to 30s between attempts.
  The last screen stays on the keys for 10s so a network blip doesn't flash the deck dark, then they go blank. A
  connection that is silent for 90s is abandoned (the daemon sends a heartbeat every 30s), and so is one that
  never answers the initial request. The backoff only starts over after a connection has held for 10s.
- **Odd daemon responses.** A state that doesn't describe a grid with the right number of keys is rejected rather
  than treated as "all blank", and server-sent events that aren't surface states are ignored.
- **Stream Deck unplugged.** The process notices (a closed event stream or a failed write), exits, and systemd
  restarts it until the device is back.
- **Hardware.** Only the original 15-key Stream Deck is supported, as in the daemon.

## Options

| Flag / variable | Default | |
|---|---|---|
| `-server` / `DESKPAD_SERVER` | none (required) | Base URL of deskpadd's web address. |
| `DESKPAD_TOKEN` | none | Bearer token for key presses (the daemon's `web.auth-token`). |
| `-long-press` | `500ms` | Hold time that counts as a long press. |
| `-idle-timeout` | `90s` | Reconnect if the event stream is silent this long. |
