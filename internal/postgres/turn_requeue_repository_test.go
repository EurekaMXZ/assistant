package postgres

import (
	"encoding/json"
	"testing"

	"github.com/EurekaMXZ/assistant/internal/domain"
)

func TestPlanStaleTurnTransitionRejectsProcessingTurn(t *testing.T) {
	transition, err := planStaleTurnTransition(staleTurnSnapshot{
		Status:   domain.TurnStatusProcessing,
		Metadata: json.RawMessage(`{"foo":"bar"}`),
	})
	if err == nil {
		t.Fatalf("processing turn transition = %#v, want error", transition)
	}
}

func TestPlanStaleTurnTransitionRequeuesContextReadyTurn(t *testing.T) {
	transition, err := planStaleTurnTransition(staleTurnSnapshot{
		Status:   domain.TurnStatusContextReady,
		Metadata: json.RawMessage(`{"foo":"bar"}`),
	})
	if err != nil {
		t.Fatalf("planStaleTurnTransition: %v", err)
	}

	if transition.Status != domain.TurnStatusAccepted {
		t.Fatalf("status = %q, want %q", transition.Status, domain.TurnStatusAccepted)
	}
	if !transition.PublishAcceptedEvent {
		t.Fatal("expected accepted requeue to publish accepted event")
	}
	var decoded map[string]any
	if err := json.Unmarshal(transition.Metadata, &decoded); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if decoded["foo"] != "bar" {
		t.Fatalf("expected metadata to survive requeue, got %#v", decoded)
	}
}
