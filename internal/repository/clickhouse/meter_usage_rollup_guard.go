package clickhouse

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	chgo "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/flexprice/flexprice/internal/clickhouse"
	"github.com/flexprice/flexprice/internal/logger"
)

const (
	chCodeUnknownIdentifier = 47
	chCodeUnknownTable      = 60
)

var rollupCheckTimeout = 5 * time.Second

// TableExistsFunc reports whether a table exists.
type TableExistsFunc func(ctx context.Context, table string) (bool, error)

// NewTableExistsFunc checks tables with EXISTS TABLE.
func NewTableExistsFunc(store *clickhouse.ClickHouseStore) TableExistsFunc {
	return func(ctx context.Context, table string) (bool, error) {
		if store == nil || !validRollupTable.MatchString(table) {
			return false, fmt.Errorf("cannot check table %q", table)
		}
		var exists uint8
		if err := store.GetConn().QueryRow(ctx, "EXISTS TABLE "+table).Scan(&exists); err != nil {
			return false, err
		}
		return exists == 1, nil
	}
}

// InitRollupTables drops missing tiers.
func InitRollupTables(ctx context.Context, exists TableExistsFunc, hourly, daily string, log *logger.Logger) (string, string) {
	if hourly == "" {
		if daily != "" {
			log.Warn(ctx, "meter_usage daily rollup needs hourly table, rollup disabled", "table", daily)
		}
		return "", ""
	}
	if !initRollupTableReady(ctx, exists, hourly, log) {
		if daily != "" {
			log.Warn(ctx, "meter_usage hourly rollup unavailable, daily tier disabled", "table", daily)
		}
		return "", ""
	}
	if daily != "" && !initRollupTableReady(ctx, exists, daily, log) {
		daily = ""
	}
	return hourly, daily
}

func initRollupTableReady(ctx context.Context, exists TableExistsFunc, table string, log *logger.Logger) bool {
	checkCtx, cancel := context.WithTimeout(ctx, rollupCheckTimeout)
	defer cancel()
	ok, err := exists(checkCtx, table)
	if err != nil {
		log.Warn(ctx, "meter_usage rollup table check failed, tier disabled", "table", table, "error", err.Error())
		return false
	}
	if !ok {
		log.Warn(ctx, "meter_usage rollup table missing, tier disabled", "table", table)
		return false
	}
	return true
}

// isMissingRollupErr matches unknown table or identifier.
func isMissingRollupErr(err error) bool {
	var ex *chgo.Exception
	if !errors.As(err, &ex) {
		return false
	}
	return ex.Code == chCodeUnknownTable || ex.Code == chCodeUnknownIdentifier
}

// rollupFallback reports raw retry.
func (r *MeterUsageRepository) rollupFallback(ctx context.Context, err error) bool {
	if !isMissingRollupErr(err) {
		return false
	}
	var tables []string
	for _, t := range []string{r.rollup.hourly, r.rollup.daily} {
		if t != "" {
			tables = append(tables, t)
		}
	}
	for _, t := range tables {
		if strings.Contains(err.Error(), t) {
			tables = []string{t}
			break
		}
	}
	for _, t := range tables {
		r.initRollupWarn(ctx, t, err)
	}
	return true
}

// initRollupWarn warns once per table.
func (r *MeterUsageRepository) initRollupWarn(ctx context.Context, table string, err error) {
	if _, seen := r.rollupWarned.LoadOrStore(table, struct{}{}); !seen {
		r.logger.Warn(ctx, "meter_usage rollup table missing at query time, using raw reads", "table", table, "error", err.Error())
	}
}
