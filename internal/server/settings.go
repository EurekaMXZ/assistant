package server

import "time"

type Settings struct {
	Address      string
	WebOrigin    string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}
