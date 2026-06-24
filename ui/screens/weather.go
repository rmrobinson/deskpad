package screens

import (
	"context"
	"fmt"
	"image"

	"github.com/rmrobinson/deskpad"
	weatherv1 "github.com/rmrobinson/weather-server/proto/weather/v1"
)

const (
	weatherHomeKeyID = 14
)

// WeatherController is the interface the weather screen uses to retrieve readings.
type WeatherController interface {
	LatestReading() *weatherv1.WeatherReading
}

// Weather displays live weather sensor readings across the deck buttons.
type Weather struct {
	iconImg    image.Image
	keys       []image.Image
	controller WeatherController

	homeScreen deskpad.Screen
}

// NewWeather creates a Weather screen and registers it on the home screen.
func NewWeather(homeScreen *Home, wc WeatherController) *Weather {
	ws := &Weather{
		iconImg:    loadAssetImage("assets/cloud-line.png"),
		keys:       make([]image.Image, 15),
		controller: wc,
		homeScreen: homeScreen,
	}

	ws.keys[weatherHomeKeyID] = homeScreen.Icon()

	homeScreen.RegisterScreen(ws)

	return ws
}

// Name returns the screen name.
func (ws *Weather) Name() string {
	return "weather"
}

// Icon returns the icon shown on the home screen for this screen.
func (ws *Weather) Icon() image.Image {
	return ws.iconImg
}

// Show populates button images from the latest reading and returns them.
func (ws *Weather) Show() []image.Image {
	r := ws.controller.LatestReading()
	if r == nil {
		for i := range ws.keys {
			if i != weatherHomeKeyID {
				ws.keys[i] = NewTextIcon("--")
			}
		}
		return ws.keys
	}

	ws.keys[0] = NewTextIcon(fmt.Sprintf("FL%.1fC", r.FeelsLikeC))
	ws.keys[1] = NewTextIcon(fmt.Sprintf("T%.1fC", r.TempC))
	ws.keys[2] = NewTextIcon(fmt.Sprintf("%.0f%%RH", r.HumidityPct))
	ws.keys[3] = NewTextIcon(fmt.Sprintf("DP%.1fC", r.DewPointC))
	ws.keys[4] = NewTextIcon(fmt.Sprintf("%.0fhPa", r.PressureHpa))
	ws.keys[5] = NewTextIcon(fmt.Sprintf("%.1fm/s", r.WindSpeedMs))
	ws.keys[6] = NewTextIcon(fmt.Sprintf("%.0fdeg", r.WindDirDeg))
	ws.keys[7] = NewTextIcon(fmt.Sprintf("g%.1fm/s", r.WindGustMs))
	ws.keys[8] = NewTextIcon(fmt.Sprintf("%.1fmm/h", r.RainMmHr))
	ws.keys[9] = NewTextIcon(fmt.Sprintf("dy%.1fmm", r.RainDailyMm))
	ws.keys[10] = NewTextIcon(fmt.Sprintf("UV%.1f", r.UvIndex))
	if r.CloudCoverPct < 0 {
		ws.keys[11] = NewTextIcon("night")
	} else {
		ws.keys[11] = NewTextIcon(fmt.Sprintf("cld%.0f%%", r.CloudCoverPct))
	}
	ws.keys[12] = NewTextIcon(fmt.Sprintf("in%.1fC", r.TempInC))
	ws.keys[13] = NewTextIcon(fmt.Sprintf("in%.0f%%", r.HumidityInPct))

	return ws.keys
}

// KeyPressed handles navigation back to home.
func (ws *Weather) KeyPressed(ctx context.Context, id int, t deskpad.KeyPressType) (deskpad.KeyPressAction, error) {
	if id == weatherHomeKeyID {
		return deskpad.KeyPressAction{
			Action:    deskpad.KeyPressActionChangeScreen,
			NewScreen: ws.homeScreen,
		}, nil
	}

	return deskpad.KeyPressAction{
		Action: deskpad.KeyPressActionNoop,
	}, nil
}
