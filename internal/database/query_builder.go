package database

import (
	"context"
	"fmt"
	"strings"

	"github.com/flexprice/flexprice/internal/types"
)

type QueryBuilder struct {
	BaseQuery   string
	Args        []interface{}
	ArgPosition int
}

func NewQueryBuilder(baseQuery string) *QueryBuilder {
	return &QueryBuilder{
		BaseQuery:   baseQuery,
		Args:        make([]interface{}, 0),
		ArgPosition: 1,
	}
}

func (qb *QueryBuilder) WithEnvironment(ctx context.Context) *QueryBuilder {
	envID := types.GetEnvironmentID(ctx)
	if envID == "" {
		return qb
	}

	// Check if query has WHERE clause
	if strings.Contains(strings.ToUpper(qb.BaseQuery), "WHERE") {
		qb.BaseQuery += fmt.Sprintf(" AND environment_id = $%d", qb.ArgPosition)
	} else {
		qb.BaseQuery += fmt.Sprintf(" WHERE environment_id = $%d", qb.ArgPosition)
	}

	qb.Args = append(qb.Args, envID)
	qb.ArgPosition++

	return qb
}

// Add more methods for handling GROUP BY, LIMIT, etc.
