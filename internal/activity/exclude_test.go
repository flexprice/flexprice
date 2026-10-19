package activity

import (
	"testing"

	"github.com/samber/lo"
	"github.com/stretchr/testify/require"
)

func TestExcludingSink_DropsExcludedScopes(t *testing.T) {
	batches := []Batch{
		{TenantID: "t_probe", EnvironmentID: "env_1"},
		{TenantID: "t_probe", EnvironmentID: "env_2"},
		{TenantID: "t_other", EnvironmentID: "env_1"},
	}
	for _, tc := range []struct {
		excluded []string
		want     []string
	}{
		{excluded: nil, want: []string{"t_probe:env_1", "t_probe:env_2", "t_other:env_1"}},
		{excluded: []string{"t_probe"}, want: []string{"t_other:env_1"}},
		{excluded: []string{" t_probe:env_1 "}, want: []string{"t_probe:env_2", "t_other:env_1"}},
	} {
		sink := &recordingSink{}
		s := excludeTenants(sink, tc.excluded)
		for _, b := range batches {
			s.Publish(b)
		}
		require.Equal(t, tc.want, lo.Map(sink.batches, func(b Batch, _ int) string { return b.TenantID + ":" + b.EnvironmentID }), tc.excluded)
	}
}
