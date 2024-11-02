package main

import (
	"context"
	"flag"
	"log"
	"os/signal"
	"syscall"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
	"github.com/godbus/dbus"
	"github.com/lawl/pulseaudio"
	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/service"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/go-mpris"
	"github.com/zmb3/spotify/v2"
)

var (
	useMPRIS           = flag.Bool("use-mpris", false, "Should try to control using the MPRIS interface?")
	pulseAudioSinkName = flag.String("pa-sink", "", "The name of the PulseAudio sink to use")
)

func configureSpotifyClient(ctx context.Context) *spotify.Client {
	sth := newSpotifyAuthHander(8037)
	token := sth.Token(ctx)

	spc := spotifyClientFromToken(token)

	user, err := spc.CurrentUser(ctx)
	if err != nil {
		log.Fatalf("unable to get spotify current user: %s\n", err.Error())
	}

	log.Printf("spotify user %s\n", user.ID)
	return spc
}

func main() {
	flag.Parse()

	// Detect and initialize the Stream Deck
	// No point in continuing if we can't find the right hardware to use.
	sd, err := sdeck.New(sdeck.StreamDeckOriginalV2)
	if err != nil {
		log.Fatalf("unable to initialize stream deck: %s\n", err.Error())
	}
	defer sd.Close()

	serial, err := sd.Serial()
	if err != nil {
		log.Fatalf("unable to get serial number: %s\n", err.Error())
	}
	log.Printf("using stream deck %s\n", serial)

	err = sd.ClearAllKeys()
	if err != nil {
		log.Fatalf("error resetting deck - consider unplugging & replugging the stream deck. Details: %s\n", err.Error())
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
	defer stop()

	// Setup Spotify. This is used as the playlist provider; and if not using the Linux MPRIS interface
	// it will also be used to control media playback.
	spotifyMP := service.NewSpotifyMediaPlayer(configureSpotifyClient(ctx))

	if err := spotifyMP.RefreshPlayerState(ctx); err != nil {
		log.Fatalf("unable to refresh spotify player state: %s\n", err.Error())
	}
	if err := spotifyMP.RefreshPlaylists(ctx); err != nil {
		log.Fatalf("unable to refresh spotify playlists: %s\n", err.Error())
	}

	var mediaPlayer service.MediaPlayerController

	if *useMPRIS {
		conn, err := dbus.SessionBus()
		if err != nil {
			panic(err)
		}
		defer conn.Close()

		names, err := mpris.List(conn)
		if err != nil {
			panic(err)
		}
		if len(names) == 0 {
			log.Fatal("No media player found.")
		}

		name := names[0]
		log.Println("Found media player:", name)

		mprisPlayer := mpris.New(conn, name)

		paClient, err := pulseaudio.NewClient()
		if err != nil {
			log.Fatalf("error connecting to pulseaudio: %s\n", err.Error())
		}
		defer paClient.Close()

		paClient.SetDefaultSink(*pulseAudioSinkName)

		mprisMP := service.NewLinuxMediaPlayer(mprisPlayer, paClient)
		mediaPlayer = mprisMP
	} else {
		mediaPlayer = spotifyMP
	}

	// Keep the Spotify playlists fresh
	go func() {
		for {
			time.Sleep(time.Hour)

			if err := spotifyMP.RefreshPlaylists(context.Background()); err != nil {
				log.Printf("unable to refresh spotify playlist: %s\n", err.Error())
			} else {
				log.Printf("playlists refreshed\n")
			}
		}
	}()

	// Set up the UI
	mps := ui.NewMediaPlayerScreen(mediaPlayer)

	mpss := ui.NewMediaPlayerSettingScreen(spotifyMP)
	mpss.RefreshDevices(ctx)

	mpls := ui.NewMediaPlaylistScreen(spotifyMP, mediaPlayer)

	hs := ui.NewHomeScreen(mps, mpls)

	mpss.SetHomeScreen(hs)
	mpss.SetPlayerScreen(mps)

	mps.SetHomeScreen(hs)
	mps.SetPlaylistScreen(mpls)
	mps.SetSettingsScreen(mpss)

	mpls.SetHomeScreen(hs)
	mpls.SetPlayerScreen(mps)

	d := deskpad.NewDeck(sd, hs)
	d.Run(ctx)
}
