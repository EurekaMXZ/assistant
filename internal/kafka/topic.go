package kafka

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"

	kafkago "github.com/segmentio/kafka-go"
)

func EnsureTopic(ctx context.Context, settings Settings) error {
	if len(settings.Brokers) == 0 {
		return errors.New("no kafka brokers configured")
	}

	conn, err := (&kafkago.Dialer{}).DialContext(ctx, "tcp", settings.Brokers[0])
	if err != nil {
		return fmt.Errorf("dial kafka broker %s: %w", settings.Brokers[0], err)
	}
	defer conn.Close()

	controller, err := conn.Controller()
	if err != nil {
		return fmt.Errorf("load kafka controller: %w", err)
	}

	controllerConn, err := (&kafkago.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(controller.Host, strconv.Itoa(controller.Port)))
	if err != nil {
		return fmt.Errorf("dial kafka controller: %w", err)
	}
	defer controllerConn.Close()

	err = controllerConn.CreateTopics(
		kafkago.TopicConfig{Topic: settings.WorkflowTopic, NumPartitions: 16, ReplicationFactor: 1},
		kafkago.TopicConfig{Topic: settings.EffectiveStreamTopic(), NumPartitions: 16, ReplicationFactor: 1},
	)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "topic with this name already exists") {
		return fmt.Errorf("create kafka topic %s: %w", settings.WorkflowTopic, err)
	}

	return nil
}
