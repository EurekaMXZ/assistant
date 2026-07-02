package worker

import (
	"testing"

	"github.com/segmentio/kafka-go"
)

func TestOffsetTrackerCommitsOnlyContiguousCompletedPrefix(t *testing.T) {
	tracker := newOffsetTracker()
	first, _ := tracker.register(kafka.Message{Topic: "workflow", Partition: 0, Offset: 10})
	second, _ := tracker.register(kafka.Message{Topic: "workflow", Partition: 0, Offset: 11})

	tracker.complete(second)
	if ready := tracker.ready(); len(ready) != 0 {
		t.Fatalf("ready offsets = %#v, want none while offset 10 is incomplete", ready)
	}
	tracker.complete(first)
	ready := tracker.ready()
	if len(ready) != 1 || ready[0].Offset != 11 {
		t.Fatalf("ready offsets = %#v, want commit through offset 11", ready)
	}
	tracker.committed(ready[0])
}

func TestOffsetTrackerAdvancesPartitionsIndependently(t *testing.T) {
	tracker := newOffsetTracker()
	blocked, _ := tracker.register(kafka.Message{Topic: "workflow", Partition: 0, Offset: 3})
	completed, _ := tracker.register(kafka.Message{Topic: "workflow", Partition: 1, Offset: 7})
	tracker.complete(completed)

	ready := tracker.ready()
	if len(ready) != 1 || ready[0].Partition != 1 || ready[0].Offset != 7 {
		t.Fatalf("ready offsets = %#v, want partition 1 offset 7", ready)
	}
	if blocked.completed {
		t.Fatal("blocked partition was unexpectedly completed")
	}
}

func TestOffsetTrackerRetriesFailedCommit(t *testing.T) {
	tracker := newOffsetTracker()
	offset, _ := tracker.register(kafka.Message{Topic: "workflow", Partition: 0, Offset: 4})
	tracker.complete(offset)
	ready := tracker.ready()
	tracker.commitFailed(ready[0])

	retry := tracker.ready()
	if len(retry) != 1 || retry[0].Offset != 4 {
		t.Fatalf("retry offsets = %#v, want offset 4", retry)
	}
}
