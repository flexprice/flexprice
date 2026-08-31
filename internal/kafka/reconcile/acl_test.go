package reconcile

import (
	"testing"

	"github.com/Shopify/sarama"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllowAllACLRules_ShapeAndClusterName(t *testing.T) {
	rules := AllowAllACLRules()
	require.Len(t, rules, 4)

	byType := map[sarama.AclResourceType]aclRule{}
	for _, r := range rules {
		byType[r.Resource().ResourceType] = r
	}

	// All four resource types present.
	for _, rt := range []sarama.AclResourceType{
		sarama.AclResourceCluster, sarama.AclResourceTopic,
		sarama.AclResourceGroup, sarama.AclResourceTransactionalID,
	} {
		require.Contains(t, byType, rt, "missing resource type %v", rt)
	}

	// Cluster MUST be kafka-cluster, not "*".
	assert.Equal(t, "kafka-cluster", byType[sarama.AclResourceCluster].Resource().ResourceName)
	// The other three are "*".
	assert.Equal(t, "*", byType[sarama.AclResourceTopic].Resource().ResourceName)
	assert.Equal(t, "*", byType[sarama.AclResourceGroup].Resource().ResourceName)
	assert.Equal(t, "*", byType[sarama.AclResourceTransactionalID].Resource().ResourceName)

	// No DelegationToken rule.
	_, hasDT := byType[sarama.AclResourceDelegationToken]
	assert.False(t, hasDT, "DelegationToken must be excluded")

	// Common shape on every rule.
	for _, r := range rules {
		assert.Equal(t, sarama.AclPatternLiteral, r.Resource().ResourcePatternType)
		assert.Equal(t, "User:*", r.Acl().Principal)
		assert.Equal(t, "*", r.Acl().Host)
		assert.Equal(t, sarama.AclOperationAll, r.Acl().Operation)
		assert.Equal(t, sarama.AclPermissionAllow, r.Acl().PermissionType)
	}
}

// fakeACLAdmin records CreateACL calls; embeds the interface so the rest of
// the (large) sarama.ClusterAdmin surface is satisfied without stubs.
type fakeACLAdmin struct {
	sarama.ClusterAdmin
	created []struct {
		R sarama.Resource
		A sarama.Acl
	}
	err error
}

func (f *fakeACLAdmin) CreateACL(r sarama.Resource, a sarama.Acl) error {
	if f.err != nil {
		return f.err
	}
	f.created = append(f.created, struct {
		R sarama.Resource
		A sarama.Acl
	}{r, a})
	return nil
}

func TestCreateACL_PassesThrough(t *testing.T) {
	fake := &fakeACLAdmin{}
	admin := &SaramaAdmin{Admin: fake}
	r := sarama.Resource{ResourceType: sarama.AclResourceTopic, ResourceName: "*", ResourcePatternType: sarama.AclPatternLiteral}
	a := sarama.Acl{Principal: "User:*", Host: "*", Operation: sarama.AclOperationAll, PermissionType: sarama.AclPermissionAllow}
	require.NoError(t, admin.CreateACL(r, a))
	require.Len(t, fake.created, 1)
	assert.Equal(t, "*", fake.created[0].R.ResourceName)
}
