# deskpad

A program to allow for customized control of a Elgato Stream deck.

## Deployment

Linux deployment files live in `deploy/`. See `deploy/README.md` for
installing `deskpadd` as a systemd `--user` service on the machine with the
Stream Deck plugged in — it must be built on that machine (see that file for
why cross-compiling it doesn't work cleanly).

## Remote Stream Deck

The Stream Deck does not have to be plugged into the machine running `deskpadd`. `cmd/deskpad-surface` runs on a
small host next to the Stream Deck (a Raspberry Pi, say), paints the daemon's keys over its HTTP API and sends key
presses back. See [deploy/surface](deploy/surface/README.md).

# TODO
