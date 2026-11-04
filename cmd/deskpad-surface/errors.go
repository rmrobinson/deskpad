package main

import "errors"

var (
	errMissingServer = errors.New("no daemon address given; set -server or DESKPAD_SERVER")
	errBadServer     = errors.New("the daemon address must be an http:// or https:// URL")
)
