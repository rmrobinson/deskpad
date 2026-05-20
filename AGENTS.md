# AGENTS.md

Guidance for coding agents working in this repository.

## Project Purpose

`deskpad` is a Go control surface system for driving desktop and nearby-device workflows from compact button-grid clients. The current primary client is a 15-button Elgato Stream Deck, but the project should be treated as a control layer that can also support other clients, including a future web client that mirrors the Stream Deck layout and icons.

The daemon in `cmd/deskpadd` wires control surfaces to optional integrations:

- Spotify for playback control, playlists, and OAuth token management.
- Linux MPRIS and PulseAudio for local media and audio output control.
- Bluetooth device settings through BlueZ.
- Divoom Timebox display control, including clock, temperature, and weather display.
- A small HTTP status API on `:1337`.

## Repository Layout

- `deck.go`: Current Stream Deck event loop, screen switching, key press timing, and icon updates.
- `screen.go`: The `deskpad.Screen` interface implemented by all UI screens.
- `cmd/deskpadd`: Main daemon, config loading, integration setup, Spotify auth, and HTTP API.
- `ui`: Shared UI domain types, such as media items and audio outputs.
- `ui/controllers`: Integration-facing controllers. These own external service calls and cached state.
- `ui/screens`: Button-grid screen implementations and embedded 72x72 assets.
- `ui/screens/assets`: Embedded PNG icons and font files.
- `example_config.yaml`: Example daemon configuration.

## Architecture Standards

- Treat the Stream Deck as one client for the control model, not as the whole product boundary.
- Follow the existing lightweight MVC-style structure:
  - Screens are the views. They define what is displayed on the button-grid UI and map key positions to actions.
  - Controllers back each screen. They contain the behavior, integration calls, and cached data needed by the screen.
  - Shared `ui` package types act as simple model/domain objects passed between controllers and screens.
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
- The current Stream Deck UI assumes 15 keys and 72x72 key images. Do not introduce a different geometry without updating the supporting assumptions throughout screens, assets, and any mirror clients.

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
- For a future web mirror, expose enough screen metadata and icons through the API so the browser can render the same current layout without duplicating screen logic.

## Configuration

The daemon uses Viper and looks for `deskpad.yaml` in:

- `$HOME/.deskpad`
- the current working directory

Use `cmd/deskpadd/example_config.yaml` as the schema reference. Feature blocks are optional where the daemon checks for missing values, but Spotify setup is currently part of startup.

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

- The HTTP API currently exposes `/status` only.
- `TODO.md` tracks planned API, web UI, Stream Deck dimension, Timebox, playlist, playback, and known bug work.
- Be careful with nil optional integrations. Some startup paths intentionally use Spotify when MPRIS/PulseAudio are disabled.
- When adding API surface, keep response types JSON-tagged and avoid leaking internal controller types directly.
- When touching Spotify auth, preserve OAuth state validation and keep token file permissions restrictive.
