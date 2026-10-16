#!/usr/bin/env bash
# Regenerates ./housepb, the vendored copy of github.com/rmrobinson/house's
# api/{,command,device,trait} Go bindings. That repo builds its proto Go code
# via Bazel and doesn't commit it, so it isn't fetchable by plain `go build`;
# this script regenerates the subset deskpad depends on whenever the
# upstream .proto files change (see go.mod's replace directive).
#
# Requires: protoc, protoc-gen-go, protoc-gen-go-grpc, buf (for fetching
# google/rpc/status.proto, which isn't bundled with protoc).
set -euo pipefail

HOUSE_SRC="${HOUSE_SRC:-$HOME/src/github.com/rmrobinson/house}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VENDOR_DIR="$SCRIPT_DIR/../housepb"
WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

buf export buf.build/googleapis/googleapis --path google/rpc/status.proto -o "$WORK_DIR/googleapis"

declare -a FILES=(api/house.proto api/bridge.proto)
for f in "$HOUSE_SRC"/api/trait/*.proto; do FILES+=("api/trait/$(basename "$f")"); done
for f in "$HOUSE_SRC"/api/device/*.proto; do FILES+=("api/device/$(basename "$f")"); done
for f in "$HOUSE_SRC"/api/command/*.proto; do FILES+=("api/command/$(basename "$f")"); done

rm -rf "$VENDOR_DIR/api"

protoc \
  -I "$HOUSE_SRC" \
  -I "$WORK_DIR/googleapis" \
  --go_out="$VENDOR_DIR" --go_opt=module=github.com/rmrobinson/house \
  --go-grpc_out="$VENDOR_DIR" --go-grpc_opt=module=github.com/rmrobinson/house \
  "${FILES[@]}"

echo "regenerated $VENDOR_DIR/api"
