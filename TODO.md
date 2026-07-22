# TODO

## API & web UI features
[ ] expose weather info via API
[x] expose stream deck dimensions to the API
[x] expose stream deck icons through the API
[x] expose media playback control through the API
[x] expose playlist info through the API
[x] expose screen navigation control through the API
[x] build a basic web UI to control media playback
[x] display virtual stream deck on web browser
[ ] build a web UI to add playlists

## Streamdeck features
[ ] support other dimensions for the Stream Deck keys

## General features
[ ] use day/nighttime to choose different Timebox weather displays
[ ] add a screen to control light on/off, colour and dim modes
[ ] when a new song starts playing, scroll the song name on the Timebox
[ ] allow an icon for a playlist to be specified

## Playback features
[ ] when playing a spotify playlist, launch from a goroutine to avoid blocking the screen

## Known Bugs
[ ] Bluetooth client displays some errors - maybe fork and fix the repo?
[x] shuffle/loop changing isn't working - fix the mpris client library
