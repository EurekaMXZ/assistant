package redis

type Settings struct {
	Addr          string
	Password      string
	DB            int
	ChannelPrefix string
}
