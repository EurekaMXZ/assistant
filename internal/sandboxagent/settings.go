package sandboxagent

import (
	"os"
	"strconv"
	"strings"
)

const (
	defaultPort           = 52
	defaultWorkdir        = "/workspace"
	defaultMaxOutputBytes = 1 << 20
)

type Settings struct {
	Port           uint32
	Workdir        string
	MaxOutputBytes int
}

func LoadSettingsFromEnv() Settings {
	return Settings{
		Port:           uint32(getenvInt("SANDBOX_AGENT_PORT", defaultPort)),
		Workdir:        getenv("SANDBOX_AGENT_WORKDIR", defaultWorkdir),
		MaxOutputBytes: getenvInt("SANDBOX_AGENT_MAX_OUTPUT_BYTES", defaultMaxOutputBytes),
	}
}

func getenv(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}
