package kafka

import (
	"time"

	kafkago "github.com/segmentio/kafka-go"
)

const workflowWriterBatchTimeout = 25 * time.Millisecond

type ReaderSettings struct {
	Brokers       []string
	WorkflowTopic string
	ConsumerGroup string
}

func NewWorkflowWriter(settings Settings) *kafkago.Writer {
	return &kafkago.Writer{
		Addr:         kafkago.TCP(settings.Brokers...),
		Topic:        settings.WorkflowTopic,
		Balancer:     &kafkago.Hash{},
		RequiredAcks: kafkago.RequireAll,
		BatchTimeout: workflowWriterBatchTimeout,
	}
}

func NewWorkflowReader(settings ReaderSettings) *kafkago.Reader {
	return kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:        settings.Brokers,
		GroupID:        settings.ConsumerGroup,
		Topic:          settings.WorkflowTopic,
		CommitInterval: 0,
		StartOffset:    kafkago.FirstOffset,
		MinBytes:       1,
		MaxBytes:       10 << 20,
	})
}
