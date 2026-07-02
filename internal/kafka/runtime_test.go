package kafka

import (
	"testing"

	kafkago "github.com/segmentio/kafka-go"
)

func TestWorkflowWriterRequiresBrokerAcknowledgment(t *testing.T) {
	writer := NewWorkflowWriter(Settings{Brokers: []string{"127.0.0.1:9092"}, WorkflowTopic: "workflow"})
	defer writer.Close()
	if writer.RequiredAcks != kafkago.RequireAll {
		t.Fatalf("required acks = %v, want RequireAll", writer.RequiredAcks)
	}
	if _, ok := writer.Balancer.(*kafkago.Hash); !ok {
		t.Fatalf("balancer type = %T, want conversation-key hash", writer.Balancer)
	}
}
