package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	sdeck "github.com/Luzifer/streamdeck"
	"github.com/godbus/dbus"
	"github.com/lawl/pulseaudio"
	"github.com/muka/go-bluetooth/bluez/profile/adapter"
	"github.com/rmrobinson/deskpad"
	"github.com/rmrobinson/deskpad/ui"
	"github.com/rmrobinson/deskpad/ui/controllers"
	"github.com/rmrobinson/deskpad/ui/screens"
	"github.com/rmrobinson/go-mpris"
	"github.com/rmrobinson/timebox"
	tbbt "github.com/rmrobinson/timebox/bluetooth"
	weatherv1 "github.com/rmrobinson/weather-server/proto/weather/v1"
	"github.com/spf13/viper"
	"github.com/zmb3/spotify/v2"
	"golang.org/x/oauth2"
)

func configureSpotifyClient(ctx context.Context, tokenFilePath string) *spotify.Client {
	tokenFile, err := os.ReadFile(tokenFilePath)
	var token *oauth2.Token

	if err == nil {
		var t oauth2.Token
		err = json.Unmarshal(tokenFile, &t)
		if err == nil {
			token = &t
		} else {
			log.Printf("failed to deserialize token: %s\n", err.Error())
		}

		log.Printf("*** using cached token from %s for spotify client\n", tokenFilePath)
	}

	if token == nil {
		sth := newSpotifyAuthHander(8037)
		token = sth.Token(ctx)

		if token == nil {
			log.Fatalf("unable to get spotify token: %s\n", err.Error())
		}

		tokenStr, err := json.Marshal(token)
		if err != nil {
			log.Fatalf("unable to marshal token to string: %s\n", err.Error())
		}

		err = os.WriteFile(tokenFilePath, tokenStr, 0600)
		if err != nil {
			log.Fatalf("unable to save creds to json file: %s\n", err.Error())
		}

		log.Printf("*** saved spotify token to %s; using new token for spotify client\n", tokenFilePath)
	}

	spc := spotifyClientFromToken(token)

	user, err := spc.CurrentUser(ctx)
	if err != nil {
		log.Fatalf("unable to get spotify current user: %s\n", err.Error())
	}

	log.Printf("spotify user %s\n", user.ID)
	return spc
}

func main() {
	viper.SetConfigName("deskpad")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("$HOME/.deskpad")
	viper.AddConfigPath(".")
	viper.SetDefault("web.addr", ":1337")

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("unable to load config file: %s\n", err.Error())
	}

	var sd *sdeck.Client
	if viper.GetBool("use-streamdeck") {
		// Detect and initialize the Stream Deck
		// No point in continuing if we can't find the right hardware to use.
		sd, err = sdeck.New(sdeck.StreamDeckOriginalV2)
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
	} else {
		log.Printf("*** Stream Deck disabled\n")
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT)
	defer stop()

	// Setup Spotify. This is used as the playlist provider; and if not using the Linux MPRIS interface
	// it will also be used to control media playback.
	spotifyClient := configureSpotifyClient(ctx, "token.json")

	var mprisConn *dbus.Conn
	var mprisInstanceName string
	var pulseAudioClient *pulseaudio.Client
	// Setup MPRIS & PulseAudio, if configured
	if viper.GetBool("use-mpris") {
		conn, err := dbus.SessionBus()
		if err != nil {
			panic(err)
		}
		mprisConn = conn

		names, err := mpris.List(conn)
		if err != nil {
			panic(err)
		}
		if len(names) == 0 {
			log.Printf("*** No MPRIS media player found; falling back to Spotify playback control\n")
			conn.Close()
			mprisConn = nil
		} else {
			mprisInstanceName = names[0]
			log.Printf("*** MPRIS media player at '%s'\n", mprisInstanceName)

			paClient, err := pulseaudio.NewClient()
			if err != nil {
				log.Printf("error connecting to pulseaudio; falling back to Spotify playback control: %s\n", err.Error())
				conn.Close()
				mprisConn = nil
			} else {
				defer conn.Close()
				defer paClient.Close()
				pulseAudioClient = paClient
			}
		}
	} else {
		log.Printf("*** MPRIS disabled\n")
	}

	// Setup weather, if configured
	var wc *controllers.Weather
	if viper.IsSet("weather.addr") {
		wc = controllers.NewWeather(
			viper.GetString("weather.addr"),
			viper.GetBool("weather.use-tls"),
			viper.GetString("weather.ca-cert"),
		)
		go wc.Run(ctx)
	} else {
		log.Printf("no weather address provided, will not check for weather updates")
	}

	// Setup Timebox, if configured
	tbAddr := viper.GetString("timebox.addr")
	var tbc *timebox.Conn
	if len(tbAddr) > 0 {
		viper.SetDefault("timebox.color.red", 0)
		viper.SetDefault("timebox.color.green", 255)
		viper.SetDefault("timebox.color.blue", 66)
		viper.SetDefault("timebox.channel", 4)

		btAddr, err := tbbt.NewAddress(tbAddr)
		if err != nil {
			log.Fatalf("invalid bluetooth address (%s): %s\n", tbAddr, err.Error())
		}

		btChann := viper.GetInt("timebox.channel")
		btConn := &tbbt.Connection{}
		err = btConn.Connect(btAddr, uint8(btChann))
		if err != nil {
			log.Fatalf("unable to connect to bluetooth device: %s\n", err.Error())
		}
		defer btConn.Close()

		tbConn := timebox.NewConn(btConn)
		if err := tbConn.Initialize(); err != nil {
			log.Fatalf("unable to establish connection with timebox: %s\n", err.Error())
		}

		tbConn.SetColor(&timebox.Colour{
			R: byte(viper.GetInt("timebox.color.red")),
			G: byte(viper.GetInt("timebox.color.green")),
			B: byte(viper.GetInt("timebox.color.blue")),
		})

		tbConn.SetBrightness(100)
		tbConn.SetTime(time.Now())

		if wc != nil {
			go func() {
				pushWeather := func() {
					r := wc.LatestReading()
					if r == nil {
						return
					}

					var conds timebox.WeatherCondition
					switch r.Condition {
					case weatherv1.WeatherCondition_WEATHER_CONDITION_SUNNY,
						weatherv1.WeatherCondition_WEATHER_CONDITION_MOSTLY_SUNNY:
						conds = timebox.WeatherDarkClear
					case weatherv1.WeatherCondition_WEATHER_CONDITION_PARTLY_CLOUDY:
						conds = timebox.WeatherDarkPartiallyCoudy
					case weatherv1.WeatherCondition_WEATHER_CONDITION_MOSTLY_CLOUDY,
						weatherv1.WeatherCondition_WEATHER_CONDITION_OVERCAST:
						conds = timebox.WeatherDarkCloudy
					case weatherv1.WeatherCondition_WEATHER_CONDITION_LIGHT_RAIN,
						weatherv1.WeatherCondition_WEATHER_CONDITION_RAIN:
						conds = timebox.WeatherDarkRain
					case weatherv1.WeatherCondition_WEATHER_CONDITION_HEAVY_RAIN:
						conds = timebox.WeatherDarkRainAndLightning
					case weatherv1.WeatherCondition_WEATHER_CONDITION_FREEZING_RAIN,
						weatherv1.WeatherCondition_WEATHER_CONDITION_SNOW:
						conds = timebox.WeatherDarkSnow
					case weatherv1.WeatherCondition_WEATHER_CONDITION_NIGHT:
						conds = timebox.WeatherDarkClear
					default:
						conds = timebox.WeatherSun
					}

					log.Printf("weather shows feels-like %0.2f C (actual %0.2f C) with condition %s\n", r.FeelsLikeC, r.TempC, r.Condition)
					tbConn.SetTemperatureAndWeather(int(r.FeelsLikeC), timebox.Celsius, conds)
				}

				pushWeather()
				for {
					select {
					case <-ctx.Done():
						return
					case <-time.After(10 * time.Minute):
					}
					pushWeather()
				}
			}()
		}

		tbc = tbConn
	}

	// Set up the Bluetooth config
	// THe adapter ID is the interface name on the system, i.e. hci0
	var btAdapter *adapter.Adapter1
	btAdapterID := viper.GetString("bluetooth.adapter-id")
	if len(btAdapterID) > 0 {
		btAdapter, err = adapter.NewAdapter1FromAdapterID(btAdapterID)
		if err != nil {
			log.Fatalf("unable to get bt adapter from ID %s: %s\n", btAdapterID, err.Error())
		}
	}

	// Retrieve any static media playlists
	var playlists []ui.MediaPlaylist
	if err := viper.UnmarshalKey("media-playlists", &playlists); err != nil {
		log.Printf("unable to retrieve playlists: %s\n", err.Error())
	}

	// Set up the UI
	hc := controllers.NewHome(tbc)
	hs := screens.NewHome(hc)

	var mps *screens.MediaPlayer
	var linuxMpc *controllers.LinuxMediaPlayer
	var spotifyMpc *controllers.SpotifyMediaPlayer
	var apiMPC MediaPlayerController
	var playlistPlaybackController controllers.PlaylistPlaybackController

	if mprisConn != nil && pulseAudioClient != nil {
		linuxMpc = controllers.NewLinuxMediaPlayer(mprisConn, mprisInstanceName, pulseAudioClient)
		mps = screens.NewMediaPlayer(hs, linuxMpc)
		apiMPC = linuxMpc
		playlistPlaybackController = linuxMpc
	} else {
		spotifyMpc = controllers.NewSpotifyMediaPlayer(ctx, spotifyClient)
		mps = screens.NewMediaPlayer(hs, spotifyMpc)
		apiMPC = spotifyMpc
		playlistPlaybackController = spotifyMpc
	}

	mpsc := controllers.NewMediaPlayerSetting(spotifyClient, pulseAudioClient)
	mpsc.RefreshAudioOutputs(ctx)

	mpss := screens.NewMediaPlayerSetting(hs, mpsc)
	mpss.SetPlayerScreen(mps)
	mps.SetSettingsScreen(mpss)

	mplc := controllers.NewMediaPlaylist(spotifyClient, playlistPlaybackController, playlists)
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

	if wc != nil {
		_ = screens.NewWeather(hs, wc)
	}

	bs := controllers.NewBluetoothSetting(btAdapter, btAdapterID)
	bs.RefreshDevices(ctx)
	_ = screens.NewBluetoothSetting(hs, bs)

	d := deskpad.NewDeck(hs)
	var streamDeckSurface *deskpad.StreamDeckSurface
	if sd != nil {
		streamDeckSurface = deskpad.NewStreamDeckSurface(sd)
		d.RegisterSurface(streamDeckSurface)
	}

	webSurface := deskpad.NewWebSurface()
	d.RegisterSurface(webSurface)
	d.RefreshScreen()

	if streamDeckSurface != nil {
		go streamDeckSurface.Run(ctx, d)
	}

	// Set up the API
	go func() {
		api := &API{
			mpc:       apiMPC,
			mplc:      mplc,
			mpsc:      mpsc,
			d:         d,
			web:       webSurface,
			authToken: viper.GetString("web.auth-token"),
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/", api.Index)
		mux.HandleFunc("/manifest.webmanifest", api.WebAsset)
		mux.HandleFunc("/service-worker.js", api.WebAsset)
		mux.HandleFunc("/icons/", api.WebAsset)
		mux.HandleFunc("/status", api.Status)
		mux.HandleFunc("/api/ui/state", api.UIState)
		mux.HandleFunc("/api/ui/events", api.UIEvents)
		mux.HandleFunc("/api/ui/keys/", api.UIPressKey)

		addr := viper.GetString("web.addr")
		log.Printf("starting http api on %s\n", addr)
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Printf("http api stopped: %s\n", err.Error())
		}
	}()

	<-ctx.Done()
	d.Clear()
}
