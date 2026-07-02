package worker

import "github.com/segmentio/kafka-go"

type topicPartition struct {
	topic     string
	partition int
}

type trackedOffset struct {
	message   kafka.Message
	completed bool
}

type partitionOffsets struct {
	pending         []*trackedOffset
	committingIndex int
	committing      bool
	committedOffset int64
	hasCommitted    bool
}

type offsetTracker struct {
	partitions map[topicPartition]*partitionOffsets
}

func newOffsetTracker() *offsetTracker {
	return &offsetTracker{partitions: make(map[topicPartition]*partitionOffsets)}
}

func (t *offsetTracker) register(message kafka.Message) (*trackedOffset, bool) {
	key := topicPartition{topic: message.Topic, partition: message.Partition}
	partition := t.partitions[key]
	if partition == nil {
		partition = &partitionOffsets{}
		t.partitions[key] = partition
	}
	if partition.hasCommitted && message.Offset <= partition.committedOffset {
		return nil, false
	}
	for _, pending := range partition.pending {
		if pending.message.Offset == message.Offset {
			return pending, false
		}
	}
	offset := &trackedOffset{message: message}
	partition.pending = append(partition.pending, offset)
	return offset, true
}

func (t *offsetTracker) complete(offset *trackedOffset) {
	if offset != nil {
		offset.completed = true
	}
}

func (t *offsetTracker) ready() []kafka.Message {
	ready := make([]kafka.Message, 0)
	for _, partition := range t.partitions {
		if partition.committing {
			continue
		}
		index := 0
		for index < len(partition.pending) && partition.pending[index].completed {
			index++
		}
		if index == 0 {
			continue
		}
		partition.committing = true
		partition.committingIndex = index
		ready = append(ready, partition.pending[index-1].message)
	}
	return ready
}

func (t *offsetTracker) committed(message kafka.Message) {
	key := topicPartition{topic: message.Topic, partition: message.Partition}
	partition := t.partitions[key]
	if partition == nil || !partition.committing {
		return
	}
	index := partition.committingIndex
	if index > len(partition.pending) {
		index = len(partition.pending)
	}
	partition.pending = partition.pending[index:]
	partition.committing = false
	partition.committingIndex = 0
	partition.committedOffset = message.Offset
	partition.hasCommitted = true
}

func (t *offsetTracker) commitFailed(message kafka.Message) {
	key := topicPartition{topic: message.Topic, partition: message.Partition}
	partition := t.partitions[key]
	if partition == nil {
		return
	}
	partition.committing = false
	partition.committingIndex = 0
}
