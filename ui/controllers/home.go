package controllers

import "github.com/rmrobinson/timebox"

type HomeDisplay int

const (
	HomeDisplayClock HomeDisplay = iota
	HomeDisplayTemperature
)

// Home is the controller for the home screen.
type Home struct {
	tbc *timebox.Conn

	currDisplay HomeDisplay
}

// NewHome creates a new controller for the home screen.
func NewHome(tbc *timebox.Conn) *Home {
	return &Home{
		tbc:         tbc,
		currDisplay: HomeDisplayClock,
	}
}

// DisplayClock displays the clock on the Timebox, if available.
func (hc *Home) DisplayClock() {
	if hc.tbc != nil {
		hc.tbc.DisplayClock(true)
	}
	hc.currDisplay = HomeDisplayClock
}

// DisplayTemperature displays the temperature on the Timebox, if available.
func (hc *Home) DisplayTemperature() {
	if hc.tbc != nil {
		hc.tbc.DisplayTemperature(true)
	}
	hc.currDisplay = HomeDisplayTemperature
}

// CurrentDisplay returns the current set display
func (hc *Home) CurrentDisplay() HomeDisplay {
	return hc.currDisplay
}
