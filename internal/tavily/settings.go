package tavily

import "time"

const defaultBaseURL = "https://api.tavily.com"

type Settings struct {
	BaseURL           string
	APIKey            string
	HTTPClientTimeout time.Duration
}
