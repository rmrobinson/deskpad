# DeskPad Linux Deployment

This directory contains deploy-time files for running DeskPad as a
**systemd `--user` service** named `deskpadd` — not a system service. It
needs your logged-in session's D-Bus (MPRIS media control), PulseAudio
socket, and raw USB HID access to the physical Stream Deck, none of which a
dedicated system account gets without extra wiring. Run it as yourself.

Build must happen on the target Linux machine itself (the one with the
Stream Deck plugged in) — `go-hid`, pulled in via `Luzifer/streamdeck`, is
CGO-only and needs `libudev`'s headers and library for whatever architecture
it's building for. Cross-compiling it from another machine (including macOS)
hits either a missing Linux C cross-toolchain or a missing `libudev`
sysroot, depending on the approach; building in place sidesteps both.

The installer creates:

- `~/.local/bin/deskpadd`
- `~/.deskpad/deskpad.yaml`
- `~/.deskpad/deskpad.env` — `SPOTIFY_ID`/`SPOTIFY_SECRET`, read by
  `zmb3/spotify/v2/auth` itself (not deskpad's own code) for both initial
  login and ongoing OAuth token refresh. Loaded via the unit's
  `EnvironmentFile=`, not baked into the tracked `.service` file.
- `~/.config/systemd/user/deskpadd.service`

Build and install from the repository root, on the target machine:

```sh
make build
deploy/install.sh
```

Or have the installer build the binary:

```sh
deploy/install.sh --build
```

To install a specific binary:

```sh
deploy/install.sh --binary ./bin/deskpadd
```

Existing `~/.deskpad/deskpad.yaml` files are preserved by default. To
replace the config with `cmd/deskpadd/example_config.yaml`:

```sh
deploy/install.sh --overwrite-config
```

## Starting on boot without logging in

A `--user` service only runs while its user has an active session unless
linger is enabled:

```sh
sudo loginctl enable-linger "$(id -un)"
```

## Verify

```sh
systemctl --user status deskpadd
journalctl --user -u deskpadd -f
```

Spotify's OAuth token (`token.json`) and the config file both live in
`~/.deskpad/`, which is `deskpadd`'s `WorkingDirectory` — the first Spotify
auth still needs to happen interactively (it opens a local callback
listener), so run `deploy/install.sh --no-start` once, run the binary by
hand to complete auth, then start the service normally.

## Updating the binary

```sh
make build
deploy/install.sh
```

### From your own machine, without SSHing in

`~/bin/build-and-deploy-service` (not part of this repo) rsyncs this repo
to the target and builds it **there**, natively — not cross-compiled, so it
doesn't hit the `libudev` wall above. Pass `--user` since this is a
`systemd --user` service:

```sh
SERVICE_DEPLOY_HOST=your.host build-and-deploy-service --user deskpadd
```

This only updates the binary on an already-installed service (via
`install.sh`, above) — it doesn't create the config, the systemd user unit,
or enable linger.

## spotifyd (local playback control)

With `playback.control: local`, deskpadd launches playlists on a Spotify Connect device and controls it over MPRIS.
Building, installing and running that receiver is covered in [spotifyd/README.md](spotifyd/README.md).
