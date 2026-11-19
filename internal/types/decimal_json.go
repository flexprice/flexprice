package types

import (
	"encoding/json"

	"github.com/shopspring/decimal"
)

// jsonDecimalNumber replaces one object field with the decimal's JSON number form.
func jsonDecimalNumber(raw []byte, field string, value *decimal.Decimal) ([]byte, error) {
	if value == nil {
		return raw, nil
	}
	number, err := json.Marshal(json.Number(value.String()))
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil, err
	}
	if _, ok := obj[field]; !ok {
		return raw, nil
	}
	obj[field] = number
	return json.Marshal(obj)
}
