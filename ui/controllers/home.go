package controllers

import "github.com/rmrobinson/timebox"

// Home is the controller for the home screen.
type Home struct {
	tbc *timebox.Conn
}

// NewHome creates a new controller for the home screen.
func NewHome(tbc *timebox.Conn) *Home {
	return &Home{
		tbc: tbc,
	}
}

// DisplayClock displays the clock on the Timebox, if available.
func (hc *Home) DisplayClock() {
	if hc.tbc != nil {
		hc.tbc.DisplayClock(true)
	}
}

// DisplayTemperature displays the temperature on the Timebox, if available.
func (hc *Home) DisplayTemperature() {
	if hc.tbc != nil {
		hc.tbc.DisplayTemperature(true)
	}
}
