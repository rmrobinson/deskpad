package main

import (
	"context"
	"flag"
	"log"
	"net/http"
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
	"github.com/rmrobinson/timebox"
	bt "github.com/rmrobinson/timebox/bluetooth"
	"github.com/zmb3/spotify/v2"
)

var (
	useMPRIS    = flag.Bool("use-mpris", false, "Should try to control using the MPRIS interface?")
	timeboxAddr = flag.String("timebox-addr", "", "The Bluetooth address of the Timebox to control")
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
	log.Printf("*** Using stream deck '%s'\n", serial)

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
	var mediaPlayerSettings service.MediaPlayerSettingController

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
		log.Printf("*** Using MPRIS media player '%s'\n", name)
		mprisPlayer := mpris.New(conn, name)

		paClient, err := pulseaudio.NewClient()
		if err != nil {
			log.Fatalf("error connecting to pulseaudio: %s\n", err.Error())
		}
		defer paClient.Close()

		linuxMP := service.NewLinuxMediaPlayer(mprisPlayer, paClient)
		mediaPlayer = linuxMP
		mediaPlayerSettings = linuxMP
	} else {
		mediaPlayer = spotifyMP
		mediaPlayerSettings = spotifyMP
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

	var tbc *timebox.Conn
	// Setup Timebox, if configured
	if len(*timeboxAddr) > 0 {
		btAddr, err := bt.NewAddress(*timeboxAddr)
		if err != nil {
			log.Fatalf("invalid bluetooth address (%s): %s\n", *timeboxAddr, err)
		}

		btConn := &bt.Connection{}
		err = btConn.Connect(btAddr, 4)
		if err != nil {
			log.Fatalf("unable to connect to bluetooth device: %s\n", err.Error())
		}
		defer btConn.Close()

		tbConn := timebox.NewConn(btConn)
		if err := tbConn.Initialize(); err != nil {
			log.Fatalf("unable to establish connection with timebox: %s\n", err.Error())
		}

		tbConn.SetColor(&timebox.Colour{R: 0, G: 255, B: 66})

		tbConn.SetBrightness(100)
		tbConn.SetTime(time.Now())
		tbc = tbConn
	}

	// Set up the UI
	hs := ui.NewHomeScreen(tbc)

	mps := ui.NewMediaPlayerScreen(hs, mediaPlayer)

	mpss := ui.NewMediaPlayerSettingScreen(hs, mediaPlayerSettings)
	mpss.SetPlayerScreen(mps)
	mpss.RefreshDevices(ctx)
	mps.SetSettingsScreen(mpss)

	mpls := ui.NewMediaPlaylistScreen(hs, spotifyMP, mediaPlayer)
	mpls.SetPlayerScreen(mps)
	mps.SetPlaylistScreen(mpls)

	if tbc != nil {
		_ = ui.NewScoreboardScreen(hs, tbc)
	}

	d := deskpad.NewDeck(sd, hs)

	// Set up the API
	go func() {
		api := &API{
			mpc:  mediaPlayer,
			mpsc: mediaPlayerSettings,
			d:    d,
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/status", api.Status)

		log.Printf("starting http api\n")
		http.ListenAndServe(":1337", mux)
	}()

	d.Run(ctx)
}
