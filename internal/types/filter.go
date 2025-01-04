package types

type Filter struct {
	Limit  int    `form:"limit,default=50"`
	Offset int    `form:"offset,default=0"`
	Status Status `form:"status,default=published"`
	Sort   string `form:"sort,default=created_at"`
	Order  string `form:"order,default=desc"`
	Expand string `form:"expand"`
}

const (
	FILTER_DEFAULT_LIMIT  = 50
	FILTER_DEFAULT_STATUS = string(StatusPublished)
	FILTER_DEFAULT_SORT   = "created_at"
	FILTER_DEFAULT_ORDER  = "desc"
)

func GetDefaultFilter() Filter {
	return Filter{
		Limit:  FILTER_DEFAULT_LIMIT,
		Offset: 0,
		Status: StatusPublished,
		Sort:   FILTER_DEFAULT_SORT,
		Order:  FILTER_DEFAULT_ORDER,
	}
}

// GetExpand returns the parsed Expand object from the filter
func (f Filter) GetExpand() Expand {
	return NewExpand(f.Expand)
}
