package cache

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedisCache_NilIsDisabled(t *testing.T) {
	var c *redisCacheImpl
	var rc RedisCache = c
	assert.False(t, rc.IsEnabled())
}
