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
	"github.com/rmrobinson/deskpad/ui/controllers"
	"github.com/rmrobinson/deskpad/ui/screens"
	"github.com/rmrobinson/go-mpris"
	"github.com/rmrobinson/timebox"
	bt "github.com/rmrobinson/timebox/bluetooth"
	"github.com/rmrobinson/weather"
	"github.com/zmb3/spotify/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	useMPRIS    = flag.Bool("use-mpris", false, "Should try to control using the MPRIS interface?")
	timeboxAddr = flag.String("timebox-addr", "", "The Bluetooth address of the Timebox to control")
	weatherAddr = flag.String("weather-addr", "", "The address of the weather service to use")
	weatherLat  = flag.Float64("weather-lat", 0, "The latitude to specify when querying for weather")
	weatherLon  = flag.Float64("weather-lon", 0, "The longitude to specify when querying for weather")
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
	spotifyClient := configureSpotifyClient(ctx)

	var mprisClient *mpris.Player
	var pulseAudioClient *pulseaudio.Client
	// Setup MPRIS & PulseAudio, if configured
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
		mprisClient = mpris.New(conn, name)

		paClient, err := pulseaudio.NewClient()
		if err != nil {
			log.Fatalf("error connecting to pulseaudio: %s\n", err.Error())
		}
		defer paClient.Close()

		pulseAudioClient = paClient
	}

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

		go func() {
			for {
				var opts []grpc.DialOption
				opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

				conn, err := grpc.NewClient(*weatherAddr, opts...)
				if err != nil {
					log.Printf("unable to connect to weather service: %s\n", err.Error())
					continue
				}
				defer conn.Close()

				weatherClient := weather.NewWeatherServiceClient(conn)
				currWeather, err := weatherClient.GetCurrentReport(context.Background(), &weather.GetCurrentReportRequest{
					Latitude:  *weatherLat,
					Longitude: *weatherLon,
				})
				if err != nil {
					log.Printf("unable to get current weather conditions: %s\n", err.Error())
					continue
				}

				var conds timebox.WeatherCondition
				switch currWeather.GetReport().GetConditions().SummaryIcon {
				case weather.WeatherIcon_SUNNY:
					conds = timebox.WeatherDarkClear
				case weather.WeatherIcon_CLOUDY:
					conds = timebox.WeatherDarkCloudy
				case weather.WeatherIcon_PARTIALLY_CLOUDY:
					conds = timebox.WeatherDarkPartiallyCoudy
				case weather.WeatherIcon_MOSTLY_CLOUDY:
					conds = timebox.WeatherDarkPartiallyCoudy
				case weather.WeatherIcon_RAIN:
					conds = timebox.WeatherDarkRain
				case weather.WeatherIcon_CHANCE_OF_RAIN:
					conds = timebox.WeatherDarkRain
				case weather.WeatherIcon_SNOW:
					conds = timebox.WeatherDarkSnow
				case weather.WeatherIcon_CHANCE_OF_SNOW:
					conds = timebox.WeatherDarkSnow
				case weather.WeatherIcon_SNOW_SHOWERS:
					conds = timebox.WeatherDarkSnow
				case weather.WeatherIcon_THUNDERSTORMS:
					conds = timebox.WeatherDarkRainAndLightning
				case weather.WeatherIcon_FOG:
					conds = timebox.WeatherDarkFog
				default:
					conds = timebox.WeatherSun
				}

				tbConn.SetTemperatureAndWeather(int(currWeather.GetReport().GetConditions().Temperature), timebox.Celsius, conds)

				time.Sleep(time.Hour)

			}
		}()

		tbc = tbConn
	}

	// Set up the UI
	hc := controllers.NewHome(tbc)
	hs := screens.NewHome(hc)

	var mps *screens.MediaPlayer
	var linuxMpc *controllers.LinuxMediaPlayer
	var spotifyMpc *controllers.SpotifyMediaPlayer

	if mprisClient != nil && pulseAudioClient != nil {
		linuxMpc = controllers.NewLinuxMediaPlayer(mprisClient, pulseAudioClient)
		mps = screens.NewMediaPlayer(hs, linuxMpc)
	} else {
		spotifyMpc = controllers.NewSpotifyMediaPlayer(ctx, spotifyClient)
		mps = screens.NewMediaPlayer(hs, spotifyMpc)
	}

	mpsc := controllers.NewMediaPlayerSetting(spotifyClient, pulseAudioClient)
	mpsc.RefreshAudioOutputs(ctx)

	mpss := screens.NewMediaPlayerSetting(hs, mpsc)
	mpss.SetPlayerScreen(mps)
	mps.SetSettingsScreen(mpss)

	mplc := controllers.NewMediaPlaylist(spotifyClient, mprisClient)
	mplc.RefreshPlaylists(ctx)

	mpls := screens.NewMediaPlaylist(hs, mplc)
	mpls.SetPlayerScreen(mps)
	mps.SetPlaylistScreen(mpls)

	// Keep the playlists fresh
	go func() {
		for {
			time.Sleep(time.Hour)

			if err := mplc.RefreshPlaylists(context.Background()); err != nil {
				log.Printf("unable to refresh spotify playlist: %s\n", err.Error())
			} else {
				log.Printf("playlists refreshed\n")
			}
		}
	}()

	if tbc != nil {
		sc := controllers.NewScoreboard(tbc)
		_ = screens.NewScoreboard(hs, sc)
	}

	d := deskpad.NewDeck(sd, hs)

	// Set up the API
	go func() {
		api := &API{
			mpc:  linuxMpc,
			mplc: mplc,
			mpsc: mpsc,
			d:    d,
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/status", api.Status)

		log.Printf("starting http api\n")
		http.ListenAndServe(":1337", mux)
	}()

	d.Run(ctx)
}
