# Deploying spotifyd

deskpad's `local` playback control needs a Spotify Connect receiver on the host that plays the audio. This directory
builds [spotifyd](https://github.com/Spotifyd/spotifyd) into a `.deb` so it is installed, upgraded and removed with
`apt`/`dpkg`, not copied into `~/.local`.

Why build instead of installing a download: spotifyd isn't in the Ubuntu or Debian archives, and its prebuilt
binaries link against OpenSSL 1.1, which current Ubuntu doesn't ship. Building here links against the system's
libraries, and apt resolves them.

Tested on Ubuntu 24.04 (aarch64) with spotifyd v0.4.2. A Spotify Premium account is required.

## 1. Build dependencies

```sh
sudo apt install build-essential libssl-dev libpulse-dev libdbus-1-dev libasound2-dev rustc-1.89 cargo-1.89
```

spotifyd 0.4.x needs Rust 1.88 or newer; Ubuntu 24.04's default `rustc` is older, hence the versioned packages.
`libasound2-dev` is only there in case a build links ALSA; the packaged feature set is PulseAudio and MPRIS.

## 2. Build the package

```sh
deploy/spotifyd/build-deb.sh v0.4.2
```

This clones the release tag, builds with `--locked` (the upstream `Cargo.lock`), derives the runtime dependencies
from the built binary, and writes `spotifyd_0.4.2-1~deskpad1_<arch>.deb` to the current directory (or `$OUT_DIR`).
It needs no root. Expect tens of minutes on small ARM hardware (about 40 on a 4-core aarch64 box); the build
is a full release compile of spotifyd and its dependencies. The package contains `/usr/bin/spotifyd`, a systemd user unit at
`/usr/lib/systemd/user/spotifyd.service`, and an example config under `/usr/share/doc/spotifyd`.

## 3. Install

```sh
sudo apt install ./spotifyd_0.4.2-1~deskpad1_arm64.deb
```

## 4. Configure

```sh
mkdir -p ~/.config/spotifyd
cp /usr/share/doc/spotifyd/spotifyd.conf.example ~/.config/spotifyd/spotifyd.conf
$EDITOR ~/.config/spotifyd/spotifyd.conf    # set cache_path to an absolute path, e.g. /home/you/.cache/spotifyd
```

Decide `cache_path` before logging in, because the credentials are stored under it. `device_name` is what deskpad's
`playback.connect-device` must match; avoid spaces.

## 5. Log in once

Login is an OAuth flow that needs a browser. On a headless host, forward its callback port to your laptop:

```sh
ssh -t -L 8000:127.0.0.1:8000 user@host spotifyd authenticate --config-path '~/.config/spotifyd/spotifyd.conf'
```

Open the link it prints, approve, and wait for "Go back to your terminal". (If port 8000 is taken, pass
`--oauth-port` and forward that one instead.) Alternatively run `spotifyd authenticate` on a machine with a browser
and copy `<cache_path>/oauth/credentials.json` to the same place on the host. Keep that file private:

```sh
chmod 700 ~/.cache/spotifyd ~/.cache/spotifyd/oauth && chmod 600 ~/.cache/spotifyd/oauth/credentials.json
```

## 6. Run it as a user service

```sh
systemctl --user enable --now spotifyd
sudo loginctl enable-linger "$USER"    # start at boot without logging in
journalctl --user -u spotifyd -f       # expect "Authenticated as '<you>'"
```

It runs as your user because it needs your PulseAudio socket and session D-Bus.

## 7. Point deskpad at it

```yaml
playback:
  control: local
  connect-device: deskpad                          # the device_name above
  mpris-prefix: org.mpris.MediaPlayer2.spotifyd
```

Restart deskpadd. Start a playlist from the deck; the first launch makes spotifyd the active device, after which
its MPRIS player (`org.mpris.MediaPlayer2.spotifyd.instance…`) appears on the session bus.

## Verifying

```sh
systemctl --user is-active spotifyd
busctl --user list | grep -i mpris       # present once spotifyd has been made the active device
```

Spotify's own device list (any Spotify client, or `GET /v1/me/player/devices`) should show `deskpad`.

## Upgrading

Rebuild with the new tag and install over the old package; the config and credentials are untouched:

```sh
deploy/spotifyd/build-deb.sh v0.4.3
sudo apt install ./spotifyd_0.4.3-1~deskpad1_arm64.deb
systemctl --user restart spotifyd
```

There is no apt repository for spotifyd, so upgrades are manual. Watch the
[releases page](https://github.com/Spotifyd/spotifyd/releases).

## Replacing a hand-installed copy

If spotifyd was previously installed by copying a release binary into `~/.local` (with a private `libssl1.1`), the
credentials in `<cache_path>` are reused, so no new login is needed:

```sh
systemctl --user disable --now spotifyd
rm -f ~/.local/bin/spotifyd ~/.config/systemd/user/spotifyd.service
rm -rf ~/.local/lib/spotifyd-libs
sudo apt install ./spotifyd_0.4.2-1~deskpad1_arm64.deb
systemctl --user daemon-reload && systemctl --user enable --now spotifyd
```

The old user unit has to go because a unit in `~/.config/systemd/user` overrides the packaged one.

## Troubleshooting

- **`libssl.so.1.1: cannot open shared object file`**: the binary is a prebuilt release, not this package.
- **No MPRIS player on the bus**: expected until spotifyd is the active device. Launch a playlist from deskpad.
- **`device 'deskpad' not found`** from deskpad: spotifyd isn't running or isn't logged in; check the journal.
- **Playback goes to the wrong place**: with `disable_discovery = true` only deskpad launches playback; check that
  `device_name` matches `playback.connect-device`.
