#!/usr/bin/env bash
set -euo pipefail

APP_NAME="deskpadd"
SERVICE_NAME="deskpadd.service"
INSTALL_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.deskpad"
USER_SYSTEMD_DIR="$HOME/.config/systemd/user"

BUILD_BINARY=0
START_SERVICE=1
OVERWRITE_CONFIG=0
BINARY_SOURCE=""

usage() {
  printf 'Usage: %s [--binary PATH | --build] [--no-start] [--overwrite-config]\n' "$0"
  printf '\n'
  printf 'Installs DeskPad as a systemd --user service running as your own\n'
  printf 'account (it needs your session D-Bus, PulseAudio, and USB HID access).\n'
  printf '\n'
  printf 'Options:\n'
  printf '  --binary PATH       Install this prebuilt binary as ~/.local/bin/deskpadd.\n'
  printf '  --build             Build ./cmd/deskpadd with go and install the result.\n'
  printf '  --no-start          Install files but do not enable or restart the service.\n'
  printf '  --overwrite-config  Replace ~/.deskpad/deskpad.yaml with example_config.yaml.\n'
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    --binary)
      if [ "$#" -lt 2 ]; then
        echo "missing path after --binary" >&2
        exit 2
      fi
      BINARY_SOURCE="$2"
      shift 2
      ;;
    --build)
      BUILD_BINARY=1
      shift
      ;;
    --no-start)
      START_SERVICE=0
      shift
      ;;
    --overwrite-config)
      OVERWRITE_CONFIG=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown argument: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [ "$(id -u)" -eq 0 ]; then
  echo "install.sh must NOT be run as root — deskpadd runs as your own user" >&2
  echo "account so it can reach your session D-Bus, PulseAudio, and USB device." >&2
  exit 1
fi

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

if [ "$BUILD_BINARY" -eq 1 ] && [ -n "$BINARY_SOURCE" ]; then
  echo "use either --binary or --build, not both" >&2
  exit 2
fi

TMP_BINARY=""
cleanup() {
  if [ -n "$TMP_BINARY" ] && [ -f "$TMP_BINARY" ]; then
    rm -f "$TMP_BINARY"
  fi
}
trap cleanup EXIT

if [ "$BUILD_BINARY" -eq 1 ]; then
  TMP_BINARY="$(mktemp "/tmp/deskpadd.XXXXXX")"
  echo "building $APP_NAME from $REPO_ROOT"
  (cd "$REPO_ROOT" && go build -o "$TMP_BINARY" ./cmd/deskpadd)
  BINARY_SOURCE="$TMP_BINARY"
elif [ -z "$BINARY_SOURCE" ]; then
  if [ -x "$REPO_ROOT/bin/deskpadd" ]; then
    BINARY_SOURCE="$REPO_ROOT/bin/deskpadd"
  else
    echo "no binary found. Run make build first, or use --build, or pass --binary PATH" >&2
    exit 1
  fi
fi

if [ ! -f "$BINARY_SOURCE" ]; then
  echo "binary does not exist: $BINARY_SOURCE" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$USER_SYSTEMD_DIR"

install -m 0755 "$BINARY_SOURCE" "$INSTALL_DIR/$APP_NAME"
install -m 0644 "$SCRIPT_DIR/$SERVICE_NAME" "$USER_SYSTEMD_DIR/$SERVICE_NAME"

if [ "$OVERWRITE_CONFIG" -eq 1 ] || [ ! -f "$CONFIG_DIR/deskpad.yaml" ]; then
  install -m 0600 "$REPO_ROOT/cmd/deskpadd/example_config.yaml" "$CONFIG_DIR/deskpad.yaml"
  echo "installed $CONFIG_DIR/deskpad.yaml from example_config.yaml — edit it before relying on this service"
else
  echo "preserving existing $CONFIG_DIR/deskpad.yaml"
fi

if [ ! -f "$CONFIG_DIR/deskpad.env" ]; then
  install -m 0600 "$SCRIPT_DIR/deskpadd.env.example" "$CONFIG_DIR/deskpad.env"
  echo "installed $CONFIG_DIR/deskpad.env from deskpadd.env.example — fill in SPOTIFY_ID/SPOTIFY_SECRET"
else
  echo "preserving existing $CONFIG_DIR/deskpad.env"
fi

systemctl --user daemon-reload

if [ "$START_SERVICE" -eq 1 ]; then
  systemctl --user enable "$SERVICE_NAME"
  systemctl --user restart "$SERVICE_NAME"
  systemctl --user --no-pager --full status "$SERVICE_NAME"
else
  echo "installed $SERVICE_NAME; start with: systemctl --user enable --now $SERVICE_NAME"
fi

echo
echo "NOTE: to start deskpadd on boot without logging in, enable linger once:"
echo "  sudo loginctl enable-linger $(id -un)"
