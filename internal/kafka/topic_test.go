package kafka

import (
	"context"
	"testing"
)

func TestEnsureTopicRequiresBrokers(t *testing.T) {
	err := EnsureTopic(context.Background(), Settings{})
	if err == nil {
		t.Fatal("expected error when no brokers are configured")
	}
	if err.Error() != "no kafka brokers configured" {
		t.Fatalf("error = %q, want %q", err.Error(), "no kafka brokers configured")
	}
}
