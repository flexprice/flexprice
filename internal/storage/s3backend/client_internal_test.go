package s3backend

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewPublicOnlyHTTPClient_NoOverallTimeout(t *testing.T) {
	assert.Zero(t, newPublicOnlyHTTPClient().Timeout)
}
