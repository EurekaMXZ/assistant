package kafka

import kafkago "github.com/segmentio/kafka-go"

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
