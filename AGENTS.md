# AGENTS.md

Guidance for coding agents working in this repository.

## Project Purpose

`deskpad` is a Go control surface system for driving desktop and nearby-device workflows from compact button-grid clients. The primary hardware client is a 15-button Elgato Stream Deck. A web client is also implemented and mirrors the Stream Deck layout and icons in a browser, including PWA support.

The daemon in `cmd/deskpadd` wires control surfaces to optional integrations:

- Spotify for playback control, playlists, and OAuth token management.
- Linux MPRIS and PulseAudio for local media and audio output control.
- Bluetooth device settings through BlueZ.
- Divoom Timebox display control, including clock, temperature, and weather display.
- A weather gRPC integration (weather-server) for current conditions.
- An HTTP server (default `:1337`) serving the web UI and a JSON API.
- A house bridge gRPC server (`bridge.addr`, optional) exposing this DeskPad instance itself as a house `MediaPlayer` device - its local media playback, volume, and screen selection - so a house aggregator can control it. This is the server side of the house protocol; the `House` controller (used by the house device screens) is the client side, talking to other bridges. The two are deliberately separate roles for now - see `ui/controllers/bridge.go`.

## Repository Layout

- `deck.go`: Deck event loop, screen switching, key press timing, icon updates, and surface fan-out.
- `screen.go`: The `deskpad.Screen` interface implemented by all UI screens.
- `surface.go`: The `deskpad.Surface` interface and `Snapshot` type shared by all surface implementations.
- `surface_streamdeck.go`: Stream Deck surface implementation.
- `surface_web.go`: Web surface — stores rendered snapshot and broadcasts updates to SSE subscribers.
- `cmd/deskpadd`: Main daemon, config loading, integration setup, Spotify auth, and HTTP server.
- `cmd/deskpadd/api.go`: HTTP handlers — web UI assets, `/status`, and the `/api/ui/` surface API.
- `cmd/deskpadd/bridge.go`: gRPC server credential setup for the house bridge listener (`bridge.*` config).
- `ui/controllers/bridge.go`: `Bridge`, the `BridgeServiceServer` implementation exposing this DeskPad instance as a house device.
- `cmd/deskpadd/web/`: Embedded web UI (HTML, PWA manifest, service worker, icons).
- `ui`: Shared UI domain types, such as media items and audio outputs.
- `ui/controllers`: Integration-facing controllers. These own external service calls and cached state.
- `ui/screens`: Button-grid screen implementations and embedded 72x72 assets.
- `ui/screens/assets`: Embedded PNG icons and font files.
- `example_config.yaml`: Example daemon configuration.

## Architecture Standards

- The Stream Deck and the web browser are two equal clients for the same control model. Neither is the primary boundary; both implement the `Surface` interface.
- Follow the existing lightweight MVC-style structure:
  - Screens are the views. They define what is displayed on the button-grid UI and map key positions to actions.
  - Controllers back each screen. They contain the behavior, integration calls, and cached data needed by the screen.
  - Shared `ui` package types act as simple model/domain objects passed between controllers and screens.
- The `Surface` interface (`surface.go`) is the rendering contract:
  - `ID() string`
  - `KeyCount() int`
  - `Refresh(Snapshot) error`
  - `UpdateKey(Snapshot, int) error`
  - `Clear() error`
- The `Deck` fans out every screen change and key update to all registered surfaces.
- `WebSurface` stores the current `Snapshot` and delivers it to SSE subscribers via `Subscribe()`.
- Keep the `deskpad.Screen` contract small and stable:
  - `Name() string`
  - `Show() []image.Image`
  - `Icon() image.Image`
  - `KeyPressed(context.Context, int, deskpad.KeyPressType) (deskpad.KeyPressAction, error)`
- Put hardware and service behavior in controllers, not screens.
- Screens should compose controller interfaces, not concrete controller implementations, unless the existing local pattern requires otherwise.
- Screens should mainly map button positions to images and key press actions.
- Do not let screens own durable service state. They may keep short-lived view snapshots, such as a copied list of audio outputs used to map a displayed key back to the selected item.
- Keep screen navigation explicit. Use `KeyPressActionChangeScreen`, `KeyPressActionRefreshScreen`, `KeyPressActionUpdateIcon`, or `KeyPressActionNoop` rather than reaching around the active client.
- The current layout assumes 15 keys and 72x72 key images. Do not introduce a different geometry without updating screens, assets, the web UI grid, and any other surface assumptions.

## HTTP API

The daemon listens on the address from `web.addr` (default `:1337`). All endpoints are in `cmd/deskpadd/api.go`.

| Path | Method | Description |
|---|---|---|
| `/` | GET | Serves the embedded web UI (`web/index.html`). |
| `/manifest.webmanifest` | GET | PWA manifest. |
| `/service-worker.js` | GET | PWA service worker. |
| `/icons/` | GET | PWA icon assets. |
| `/status` | GET | JSON status: current screen, media player state, audio outputs. |
| `/api/ui/state` | GET | JSON snapshot of the current surface grid (screen name, rows, columns, base64-PNG keys). |
| `/api/ui/events` | GET | SSE stream of surface snapshots; heartbeat every 30 s. |
| `/api/ui/keys/{id}/press` | POST | Simulate a key press (`{"type":"short"\|"long"}`). Requires `Authorization: Bearer <token>` when `web.auth-token` is set. |

The `UIStateResponse` type encodes each key image as a `data:image/png;base64,…` data URL so the browser can render them directly.

## House Bridge Server

When `bridge.addr` is set, the daemon also runs a gRPC server implementing the house `BridgeService` (`housepb/api/bridge_grpc.pb.go`), letting a house aggregator control this DeskPad instance the way it controls any other bridge on the mesh.

- `ui/controllers/bridge.go` (`Bridge`) is the implementation. It exposes exactly one `device.MediaPlayer` device, backed by whichever local media player controller (`LinuxMediaPlayer` or `SpotifyMediaPlayer`) is active:
  - **Media trait** — play/pause, fixed-step fast-forward/rewind via `SeekRelative`'s sign. There is no house `Command` for an explicit track skip yet, so `Media_Attributes.CanSkipForward/CanSkipBackward` are reported `false` even though the local players support `Next()`/`Previous()`.
  - **Volume trait** — reads/writes whatever the active controller already treats as "the" volume (PulseAudio for `LinuxMediaPlayer`, the active Spotify Connect device for `SpotifyMediaPlayer`), via `GetVolume()`/`SetVolume()` on `BridgeMediaPlayerController`.
  - **App trait** — the registered screens (`Home.Screens()`, prefixed with `Home` itself as the default/`AppClose` target) as selectable "applications"; `AppLaunch`/`AppClose` call `Deck.ChangeScreen`.
- `StreamUpdates` pushes a `DeviceUpdate` on every `ExecuteCommand`/`ExecuteCommandAsync`, plus a 2s poll (`bridgePollInterval`) that only runs while at least one subscriber is attached, so changes made outside a bridge command (a physical key press, pausing Spotify from a phone) still reach subscribers.
- `cmd/deskpadd/bridge.go` builds the server's `grpc.ServerOption`s from `bridge.*` config; TLS is optional and, when enabled with `bridge.ca-cert` set, requires a verified client certificate (mutual TLS).

## Coding Standards

- Target Go `1.22.x`, matching `go.mod`.
- Run `gofmt` on changed Go files.
- Prefer standard Go error handling and `log.Printf` for recoverable integration failures.
- Use `log.Fatalf` only for startup failures or states where the daemon cannot reasonably continue.
- Pass `context.Context` through operations that call external services or may block.
- Keep key handling fast. Long-running playback or network operations should be moved out of the synchronous key path where practical.
- Protect deck state with the existing `Deck` mutex pattern when changing the active screen or handling key presses.
- Do not commit local runtime files:
  - `cmd/deskpadd/deskpad.yaml`
  - `cmd/deskpadd/token.json`
  - `.env`
- Preserve existing user edits. The worktree may be dirty.

## UI and Asset Standards

- Use embedded assets through `ui/screens/assets.go`; do not read button-grid UI assets from the filesystem at runtime.
- Keep current button art at 72x72 pixels unless adding generalized client dimension support.
- Use `NewTextIcon` or `NewTextIconWithBackground` for text rendered onto keys.
- Prefer existing icon style and naming in `ui/screens/assets` when adding button images.
- Register new navigable screens with `Home.RegisterScreen` through the screen constructor pattern used by existing screens.
- Keep button positions as named constants near the top of each screen file.
- Web UI assets live in `cmd/deskpadd/web/` and are embedded at build time via `//go:embed`.

## Configuration

The daemon uses Viper and looks for `deskpad.yaml` in:

- `$HOME/.deskpad`
- the current working directory

Use `cmd/deskpadd/example_config.yaml` as the schema reference. Feature blocks are optional where the daemon checks for missing values, but Spotify setup is currently part of startup.

Notable config keys:
- `use-streamdeck`: bool — attach to a physical Stream Deck.
- `playback.control`: `spotify` or `local` (default `spotify`). Spotify is involved either way; this chooses how much of the control it does.
  - `spotify`: everything goes through the Spotify Web API (`SpotifyMediaPlayer`). Needs no local player and works on any OS, but controls whichever device Spotify considers active.
  - `local`: Spotify only launches media. Transport, state and volume are local, over MPRIS and PulseAudio (`LinuxMediaPlayer`). Linux only; needs the D-Bus session bus and PulseAudio at startup, but the MPRIS player itself may appear later.
- `playback.connect-device`: string — `local` only. Spotify Connect device name (e.g. `deskpad`). When set, playlists are launched on it through deskpad's own Spotify app instead of asking the MPRIS player to open the URI. A failure is returned, never retried through `OpenUri`. A Connect receiver such as spotifyd only exposes MPRIS once it is the active device, so launching has to come from Spotify.
- `playback.mpris-prefix`: string — `local` only. Only control MPRIS players whose D-Bus name starts with this (e.g. `org.mpris.MediaPlayer2.spotifyd`). Unset means any player on the session bus.
- `use-mpris`: deprecated. Honoured only when `playback.control` is unset: `true` is `local`, `false` is `spotify`.
- `web.addr`: string — HTTP listen address (default `:1337`).
- `web.auth-token`: string — bearer token required for `POST /api/ui/keys/{id}/press`; if empty, key presses from the web are disabled.
- `weather.addr`, `weather.use-tls`, `weather.ca-cert`: weather-server gRPC config.
- `timebox.addr`, `timebox.channel`, `timebox.color.*`: Timebox Bluetooth config.
- `bluetooth.adapter-id`: BlueZ adapter name (e.g. `hci0`).
- `house.addr`: string — the house bridge/aggregator to dial for the house device control screens (`ui/controllers/house.go`). Unset skips that feature entirely. This is the *client* direction — DeskPad consuming another bridge. `bridge.*` below is the opposite direction.
- `house.rooms`: list of `{room-id, label}` — one device-list/device-control screen pair per entry; `label` falls back to `room-id` if omitted.
- `house.use-tls`, `house.ca-cert`, `house.cert-file`, `house.key-file`: mutual TLS for the `house.addr` connection. house's own bridges/`housed` are issued certs by a private step-ca instance (see the `house-config` repo's `docker-compose/README.md` "Mutual TLS" section) — `ca-cert` is that CA's root, `cert-file`/`key-file` are DeskPad's own client identity from the same provisioner.
- `house.server-name`: string — overrides the hostname the server certificate is verified against. **Required** whenever `house.addr` isn't itself that name, which is the common case here: step-ca issues certs for `<name>.<host>.house.internal` (e.g. `housed.yourhost.house.internal`), essentially never the real address (`yourhost.example.com:<port>`, an IP, etc.) actually dialed. Leaving this unset when it's needed fails TLS verification, not just a config nicety.
- `bridge.addr`: string — gRPC listen address for the house bridge server; unset disables it. This is the *server* direction — DeskPad exposing itself as a bridge for a house aggregator to dial into.
- `bridge.device-id`, `bridge.device-name`: stable device ID (defaults to hostname) and display name for the device this DeskPad instance exposes.
- `bridge.use-tls`, `bridge.cert-file`, `bridge.key-file`, `bridge.ca-cert`: house bridge server TLS config; setting `ca-cert` additionally requires and verifies a client certificate (mutual TLS).

Sensitive or machine-local config and OAuth token files are ignored by git. Do not add real tokens, device addresses, or personal config values to tracked files.

## Build and Verification

From the repository root:

```sh
go test ./...
```

There are currently no test files, so this command mostly provides compile coverage across the packages.

For daemon startup checks, run from `cmd/deskpadd` with a local `deskpad.yaml`:

```sh
go run .
```

Hardware-dependent behavior may require a connected Stream Deck, Bluetooth adapter, PulseAudio session, Spotify credentials, or a Timebox device depending on the enabled config.

## Development Notes

- `TODO.md` tracks planned work including Stream Deck dimension support, Timebox, playlist, playback, and known bugs.
- Be careful with nil optional integrations. Spotify playback control is used only when `playback.control` is `spotify`; there is no automatic fallback between the two modes.
- When adding API surface, keep response types JSON-tagged and avoid leaking internal controller types directly.
- When touching Spotify auth, preserve OAuth state validation and keep token file permissions restrictive.
