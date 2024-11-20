package controllers

import "github.com/rmrobinson/timebox"

// Scoreboard is a controller for the scoreboard functionality on the Timebox, if available
type Scoreboard struct {
	redScore  int
	blueScore int

	tbc *timebox.Conn
}

// NewScoreboard creates a new scoreboard controller using the provided Timebox connection.
func NewScoreboard(tbc *timebox.Conn) *Scoreboard {
	return &Scoreboard{
		redScore:  0,
		blueScore: 0,
		tbc:       tbc,
	}
}

// Display refreshes the score on the timebox, if available
func (sc *Scoreboard) Display() {
	if sc.tbc != nil {
		sc.tbc.DisplayScoreboard(sc.redScore, sc.blueScore)
	}
}

// IncrementRedScore adds 1 to the red score
func (sc *Scoreboard) IncrementRedScore() {
	sc.redScore++
	sc.Display()
}

// DecrementRedScore removes 1 from the red score, until at 0
func (sc *Scoreboard) DecrementRedScore() {
	if sc.redScore <= 0 {
		return
	}

	sc.redScore--
	sc.Display()
}

// IncrementBlueScore adds 1 to the blue score
func (sc *Scoreboard) IncrementBlueScore() {
	sc.blueScore++
	sc.Display()
}

// DecrementBlueScore removes 1 from the blue score, until at 0
func (sc *Scoreboard) DecrementBlueScore() {
	if sc.blueScore <= 0 {
		return
	}

	sc.blueScore--
	sc.Display()
}

// ResetRedScore sets the red score to 0
func (sc *Scoreboard) ResetRedScore() {
	sc.redScore = 0
	sc.Display()
}

// ResetBlueScore sets the blue score to 0
func (sc *Scoreboard) ResetBlueScore() {
	sc.blueScore = 0
	sc.Display()
}
