package bootstrap

import (
	"context"
	"fmt"

	streamredis "github.com/EurekaMXZ/assistant/internal/redis"
)

func buildStreamHub(ctx context.Context, settings streamredis.Settings) (streamRuntime, error) {
	stream := streamredis.New(settings)
	if err := stream.Ping(ctx); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	return stream, nil
}
