package kafka

type Settings struct {
	Brokers       []string
	WorkflowTopic string
	StreamTopic   string
}

func (s Settings) EffectiveStreamTopic() string {
	if s.StreamTopic != "" {
		return s.StreamTopic
	}
	return s.WorkflowTopic + "-stream"
}
