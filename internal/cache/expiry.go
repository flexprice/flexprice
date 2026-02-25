package cache

import "time"

const (
	ExpiryDefaultInMemory   = 30 * time.Minute
	ExpiryDefaultRedis      = 5 * time.Minute
	ExpiryWalletBalance     = 30 * time.Minute
	ExpiryWalletAlertCheck  = 1 * time.Minute
	ExpiryPriceSyncLock     = 2 * time.Hour
)
