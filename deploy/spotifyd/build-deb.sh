#!/usr/bin/env bash
# Builds spotifyd from an upstream release tag and packages it as a .deb, so it is installed and removed through
# dpkg/apt instead of being copied into ~/.local.
#
# spotifyd isn't in the Ubuntu or Debian archives and its prebuilt binaries link against OpenSSL 1.1, which
# current Ubuntu no longer ships. Building here links against the system's libraries and lets apt resolve them.
#
# Build dependencies (Ubuntu 24.04):
#   sudo apt install build-essential libssl-dev libpulse-dev libdbus-1-dev libasound2-dev rustc-1.89 cargo-1.89
#
# Usage: build-deb.sh [TAG]      (default TAG: v0.4.2; the .deb is written to $OUT_DIR, default: the current dir)
set -euo pipefail

TAG="${1:-v0.4.2}"
OUT_DIR="${OUT_DIR:-$PWD}"
# spotifyd 0.4.x needs Rust 1.88 or newer; Ubuntu 24.04's default rustc is older, hence the versioned packages.
RUSTC_BIN="${RUSTC_BIN:-rustc-1.89}"
CARGO_BIN="${CARGO_BIN:-cargo-1.89}"
FEATURES="pulseaudio_backend,dbus_mpris"
REVISION="1~deskpad1"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

if [ "$(id -u)" -eq 0 ]; then
  echo "build-deb.sh must not be run as root; it only needs sudo for the final 'apt install'" >&2
  exit 1
fi

for tool in git dpkg-deb dpkg ldd readelf "$CARGO_BIN" "$RUSTC_BIN"; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "missing required tool: $tool (see the build dependencies at the top of this script)" >&2
    exit 1
  fi
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "fetching spotifyd $TAG"
git clone --quiet --depth 1 --branch "$TAG" https://github.com/Spotifyd/spotifyd "$WORK/src"

echo "building (tens of minutes on small ARM hardware)"
(
  cd "$WORK/src"
  RUSTC="$(command -v "$RUSTC_BIN")" "$CARGO_BIN" build --release --locked \
    --no-default-features --features "$FEATURES"
)

BIN="$WORK/src/target/release/spotifyd"
VERSION="${TAG#v}"
ARCH="$(dpkg --print-architecture)"

# Map each shared library the binary links directly to the package that owns it. ldd prints /lib/... paths, which
# dpkg doesn't know on a merged-/usr system, so resolve the real path first. Transitive libraries are their owners'
# own dependencies and don't belong here.
LDD_OUT="$(ldd "$BIN")"
PACKAGES=()
while read -r soname; do
  path="$(awk -v n="$soname" '$1 == n {print $3}' <<<"$LDD_OUT")"
  [ -n "$path" ] || continue
  owner="$(dpkg -S "$(readlink -f "$path")" 2>/dev/null | head -n1 | cut -d: -f1 || true)"
  if [ -z "$owner" ]; then
    echo "warning: no package owns $path ($soname); it will not be listed as a dependency" >&2
    continue
  fi
  PACKAGES+=("$owner")
done < <(readelf -d "$BIN" | awk '/\(NEEDED\)/{gsub(/[\[\]]/, "", $NF); print $NF}')

if [ "${#PACKAGES[@]}" -eq 0 ]; then
  echo "could not work out the runtime dependencies from the built binary" >&2
  exit 1
fi
DEPS="$(printf '%s\n' "${PACKAGES[@]}" | sort -u | paste -sd, - | sed 's/,/, /g')"

STAGE="$WORK/stage"
install -Dm0755 "$BIN" "$STAGE/usr/bin/spotifyd"
install -Dm0644 "$SCRIPT_DIR/spotifyd.service" "$STAGE/usr/lib/systemd/user/spotifyd.service"
install -Dm0644 "$SCRIPT_DIR/spotifyd.conf.example" "$STAGE/usr/share/doc/spotifyd/spotifyd.conf.example"
install -Dm0644 "$WORK/src/LICENSE" "$STAGE/usr/share/doc/spotifyd/copyright"

mkdir -p "$STAGE/DEBIAN"
cat > "$STAGE/DEBIAN/control" <<CONTROL
Package: spotifyd
Version: ${VERSION}-${REVISION}
Architecture: ${ARCH}
Maintainer: local build <root@localhost>
Installed-Size: $(du -sk "$STAGE/usr" | cut -f1)
Depends: ${DEPS}
Recommends: pulseaudio | pipewire-pulse
Section: sound
Priority: optional
Homepage: https://github.com/Spotifyd/spotifyd
Description: Spotify Connect receiver controlled over MPRIS
 Built locally from upstream tag ${TAG} with the ${FEATURES} features.
 Runs as a systemd user service; see /usr/share/doc/spotifyd.
CONTROL

mkdir -p "$OUT_DIR"
DEB="$OUT_DIR/spotifyd_${VERSION}-${REVISION}_${ARCH}.deb"
dpkg-deb --root-owner-group --build "$STAGE" "$DEB" >/dev/null

echo
dpkg-deb --info "$DEB" | sed -n '/Package:/,/Description:/p'
echo
echo "built $DEB"
echo "install it with: sudo apt install $DEB"
