package openai

import "time"

type Settings struct {
	UserAgent         string
	HTTPClientTimeout time.Duration
}
