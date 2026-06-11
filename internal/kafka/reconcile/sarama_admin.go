package reconcile

import "github.com/Shopify/sarama"

type SaramaAdmin struct {
	Admin sarama.ClusterAdmin
}

func toLiveTopics(in map[string]sarama.TopicDetail) map[string]liveTopic {
	out := make(map[string]liveTopic, len(in))
	for name, d := range in {
		out[name] = liveTopic{Partitions: d.NumPartitions, ReplicationFactor: d.ReplicationFactor}
	}
	return out
}

func (s *SaramaAdmin) ListTopics() (map[string]liveTopic, error) {
	details, err := s.Admin.ListTopics()
	if err != nil {
		return nil, err
	}
	return toLiveTopics(details), nil
}

func (s *SaramaAdmin) CreateTopic(name string, partitions int32, rf int16) error {
	return s.Admin.CreateTopic(name, &sarama.TopicDetail{
		NumPartitions:     partitions,
		ReplicationFactor: rf,
	}, false)
}

func (s *SaramaAdmin) CreatePartitions(name string, count int32) error {
	return s.Admin.CreatePartitions(name, count, nil, false)
}
