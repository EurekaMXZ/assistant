package redis

import "time"

type Settings struct {
	Addr          string
	Password      string
	DB            int
	ChannelPrefix string
	ReplayTTL     time.Duration
	ContextPrefix string
	ContextTTL    time.Duration
}
