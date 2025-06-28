package types

// AggregationType is a type for the type of aggregation to be performed on a meter
// This is used to determine which aggregator to use when querying the database
type AggregationType string

const (
	AggregationCount             AggregationType = "COUNT"
	AggregationSum               AggregationType = "SUM"
	AggregationAvg               AggregationType = "AVG"
	AggregationCountUnique       AggregationType = "COUNT_UNIQUE"
	AggregationLatest            AggregationType = "LATEST"
	AggregationSumWithMultiplier AggregationType = "SUM_WITH_MULTIPLIER" // Sum with a multiplier - [sum(value) * multiplier]

)

func (t AggregationType) Validate() bool {
	switch t {
	case AggregationCount, AggregationSum, AggregationAvg, AggregationCountUnique, AggregationLatest, AggregationSumWithMultiplier:
		return true
	default:
		return false
	}
}

// RequiresField returns true if the aggregation type requires a field
func (t AggregationType) RequiresField() bool {
	switch t {
	case AggregationCount:
		return false
	default:
		return true
	}
}
