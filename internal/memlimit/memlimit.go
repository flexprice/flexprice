// Package memlimit sets the Go runtime memory limit from the container's cgroup memory limit,
// so the garbage collector runs before the container is OOM-killed.
package memlimit

import (
	"context"
	"errors"
	"os"
	"runtime/debug"
	"strconv"
	"strings"

	"github.com/flexprice/flexprice/internal/logger"
)

// limitRatio leaves headroom for memory the Go runtime does not manage (stacks, cgo, OS buffers).
const limitRatio = 0.8

var cgroupLimitFiles = []string{
	"/sys/fs/cgroup/memory.max",                   // cgroup v2
	"/sys/fs/cgroup/memory/memory.limit_in_bytes", // cgroup v1
}

// Apply sets the runtime memory limit to limitRatio of the container's memory limit.
// It does nothing when GOMEMLIMIT is set explicitly or the container has no memory limit.
func Apply(ctx context.Context, log *logger.Logger) {
	if v := os.Getenv("GOMEMLIMIT"); v != "" {
		log.Info(ctx, "memlimit: using GOMEMLIMIT from environment", "gomemlimit", v)
		return
	}

	containerLimit, file, err := readCgroupLimit(cgroupLimitFiles)
	if err != nil {
		log.Info(ctx, "memlimit: no container memory limit found, leaving runtime limit unset", "reason", err.Error())
		return
	}

	limit := int64(float64(containerLimit) * limitRatio)
	debug.SetMemoryLimit(limit)
	log.Info(ctx, "memlimit: runtime memory limit set from container limit",
		"container_limit_bytes", containerLimit,
		"runtime_limit_bytes", limit,
		"ratio", limitRatio,
		"source", file)
}

// readCgroupLimit returns the first finite memory limit found in files.
func readCgroupLimit(files []string) (int64, string, error) {
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		limit, ok := parseCgroupLimit(string(raw))
		if !ok {
			return 0, f, errors.New("container memory is unlimited")
		}
		return limit, f, nil
	}
	return 0, "", errors.New("no cgroup memory limit file")
}

// parseCgroupLimit parses a cgroup memory limit. cgroup v2 writes "max" and cgroup v1 writes
// a value near MaxInt64 when there is no limit; both report ok=false.
func parseCgroupLimit(raw string) (int64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || s == "max" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 || v >= 1<<62 {
		return 0, false
	}
	return v, true
}
