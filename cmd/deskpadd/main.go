package main

import (
	"context"
	"log"
	"net/http"
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
	"github.com/rmrobinson/weather"
	"github.com/spf13/viper"
	"github.com/zmb3/spotify/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
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
	viper.SetConfigName("deskpad")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("$HOME/.deskpad")
	viper.AddConfigPath(".")

	err := viper.ReadInConfig()
	if err != nil {
		log.Fatalf("unable to load config file: %s\n", err.Error())
	}

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
	var mprisInstanceName string
	var pulseAudioClient *pulseaudio.Client
	// Setup MPRIS & PulseAudio, if configured
	if viper.GetBool("use-mpris") {
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

		mprisInstanceName = names[0]
		log.Printf("*** Using MPRIS media player '%s'\n", mprisInstanceName)
		mprisClient = mpris.New(conn, mprisInstanceName)

		paClient, err := pulseaudio.NewClient()
		if err != nil {
			log.Fatalf("error connecting to pulseaudio: %s\n", err.Error())
		}
		defer paClient.Close()

		pulseAudioClient = paClient
	}

	// Setup Timebox, if configured
	tbAddr := viper.GetString("timebox.addr")
	var tbc *timebox.Conn
	if len(tbAddr) > 0 {
		viper.SetDefault("timebox.color.red", 0)
		viper.SetDefault("timebox.color.green", 255)
		viper.SetDefault("timebox.color.blue", 66)

		btAddr, err := tbbt.NewAddress(tbAddr)
		if err != nil {
			log.Fatalf("invalid bluetooth address (%s): %s\n", tbAddr, err.Error())
		}

		btConn := &tbbt.Connection{}
		err = btConn.Connect(btAddr, 4)
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

		go func() {
			if !viper.IsSet("weather.addr") {
				log.Printf("no weather address provided, will not check for weather updates")
				return
			}

			viper.SetDefault("weather.latitude", 0)
			viper.SetDefault("weather.longitude", 0)

			for {
				time.Sleep(time.Minute * 10)

				var opts []grpc.DialOption
				opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))

				conn, err := grpc.NewClient(viper.GetString("weather.addr"), opts...)
				if err != nil {
					log.Printf("unable to connect to weather service: %s\n", err.Error())
					continue
				}
				defer conn.Close()

				weatherClient := weather.NewWeatherServiceClient(conn)
				currWeather, err := weatherClient.GetCurrentReport(context.Background(), &weather.GetCurrentReportRequest{
					Latitude:  viper.GetFloat64("weather.latitude"),
					Longitude: viper.GetFloat64("weather.longitude"),
				})
				if err != nil {
					log.Printf("unable to get current weather conditions: %s\n", err.Error())
					continue
				} else if currWeather.GetReport() == nil {
					log.Printf("empty weather report\n")
					continue
				} else if currWeather.GetReport().GetConditions() == nil {
					log.Printf("empty weather conditions\n")
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

				log.Printf("weather shows %0.2f C with condition of %d\n", currWeather.GetReport().GetConditions().Temperature, conds)

				tbConn.SetTemperatureAndWeather(int(currWeather.GetReport().GetConditions().Temperature), timebox.Celsius, conds)
			}
		}()

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

	if mprisClient != nil && pulseAudioClient != nil {
		linuxMpc = controllers.NewLinuxMediaPlayer(mprisClient, mprisInstanceName, pulseAudioClient)
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

	mplc := controllers.NewMediaPlaylist(spotifyClient, mprisClient, playlists)
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

	bs := controllers.NewBluetoothSetting(btAdapter, btAdapterID)
	bs.RefreshDevices(ctx)
	_ = screens.NewBluetoothSetting(hs, bs)

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
