package synthetic

import "context"

type Check interface {
	Name() string
	Kind() Kind
	Run(ctx context.Context) error
}
