package reconcile

import (
	"strconv"

	"github.com/Shopify/sarama"
)

const retentionMsConfigKey = "retention.ms"

type SaramaAdmin struct {
	Admin sarama.ClusterAdmin
}

// toLiveTopics reads partitions/RF from ListTopics and retention.ms via a
// per-topic DescribeConfig call (ListTopics does not return topic configs).
func (s *SaramaAdmin) toLiveTopics(in map[string]sarama.TopicDetail) (map[string]liveTopic, error) {
	out := make(map[string]liveTopic, len(in))
	for name, d := range in {
		lt := liveTopic{Partitions: d.NumPartitions, ReplicationFactor: d.ReplicationFactor}
		entries, err := s.Admin.DescribeConfig(sarama.ConfigResource{Type: sarama.TopicResource, Name: name})
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.Name == retentionMsConfigKey && e.Value != "" {
				if ms, perr := strconv.ParseInt(e.Value, 10, 64); perr == nil {
					lt.RetentionMs = ms
				}
			}
		}
		out[name] = lt
	}
	return out, nil
}

func (s *SaramaAdmin) ListTopics() (map[string]liveTopic, error) {
	details, err := s.Admin.ListTopics()
	if err != nil {
		return nil, err
	}
	return s.toLiveTopics(details)
}

func (s *SaramaAdmin) CreateTopic(name string, partitions int32, rf int16, retentionMs int64) error {
	retention := strconv.FormatInt(retentionMs, 10)
	return s.Admin.CreateTopic(name, &sarama.TopicDetail{
		NumPartitions:     partitions,
		ReplicationFactor: rf,
		ConfigEntries:     map[string]*string{retentionMsConfigKey: &retention},
	}, false)
}

func (s *SaramaAdmin) CreatePartitions(name string, count int32) error {
	return s.Admin.CreatePartitions(name, count, nil, false)
}

// CreateACL creates a single ACL binding. CreateACL on an already-present ACL
// is a broker no-op, so callers can re-create the full allow-all set idempotently.
func (s *SaramaAdmin) CreateACL(r sarama.Resource, a sarama.Acl) error {
	return s.Admin.CreateACL(r, a)
}

// aclRule is one resource+acl binding to seed.
type aclRule struct {
	resource sarama.Resource
	acl      sarama.Acl
}

func (r aclRule) Resource() sarama.Resource { return r.resource }

func (r aclRule) Acl() sarama.Acl { return r.acl }

// AllowAllACLRules returns the four allow-everyone bindings that reproduce the
// effect of allow.everyone.if.no.acl.found=true for the resource types an MSK
// SASL/SCRAM app uses. Cluster MUST be "kafka-cluster" (the only valid cluster
// resource name); a "*" there matches nothing. DelegationToken is excluded by
// design (owner-keyed, unused by MSK SASL apps).
func AllowAllACLRules() []aclRule {
	allow := sarama.Acl{
		Principal:      "User:*",
		Host:           "*",
		Operation:      sarama.AclOperationAll,
		PermissionType: sarama.AclPermissionAllow,
	}
	rule := func(rt sarama.AclResourceType, name string) aclRule {
		return aclRule{
			resource: sarama.Resource{
				ResourceType:        rt,
				ResourceName:        name,
				ResourcePatternType: sarama.AclPatternLiteral,
			},
			acl: allow,
		}
	}
	return []aclRule{
		rule(sarama.AclResourceCluster, "kafka-cluster"),
		rule(sarama.AclResourceTopic, "*"),
		rule(sarama.AclResourceGroup, "*"),
		rule(sarama.AclResourceTransactionalID, "*"),
	}
}
