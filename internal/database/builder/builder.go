package builder

import (
	sq "github.com/Masterminds/squirrel"
)

// Builder wraps Squirrel's StatementBuilderType with context-aware filtering
type Builder struct {
	sq.StatementBuilderType
}

// New creates a new Builder instance with Postgres placeholders
func New() *Builder {
	return &Builder{
		StatementBuilderType: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Select creates a SelectBuilder with automatic tenant and environment filtering
func (b *Builder) Select(columns ...string) SelectBuilder {
	return SelectBuilder{
		SelectBuilder: b.StatementBuilderType.Select(columns...),
	}
}

// Insert creates an InsertBuilder with automatic tenant handling
func (b *Builder) Insert(into string) InsertBuilder {
	return InsertBuilder{
		InsertBuilder: b.StatementBuilderType.Insert(into),
	}
}
