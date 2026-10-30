package memlimit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCgroupLimit(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		want   int64
		wantOK bool
	}{
		{name: "cgroup v2 limit", raw: "2147483648\n", want: 2147483648, wantOK: true},
		{name: "cgroup v2 unlimited", raw: "max\n", wantOK: false},
		{name: "cgroup v1 limit", raw: "1073741824", want: 1073741824, wantOK: true},
		{name: "cgroup v1 unlimited", raw: "9223372036854771712", wantOK: false},
		{name: "garbage", raw: "abc", wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseCgroupLimit(tt.raw)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}
