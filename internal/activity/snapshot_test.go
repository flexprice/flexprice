package activity

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// encodedState is the snapshot spelled with encoding/json: the struct's JSON minus dropped keys, redacted keys masked.
func encodedState(t *testing.T, r *Registration, row any) []byte {
	raw, err := json.Marshal(r.Snapshot.convert(row).Interface())
	require.NoError(t, err)
	fields := map[string]json.RawMessage{}
	require.NoError(t, json.Unmarshal(raw, &fields))
	for _, f := range r.Snapshot.fields {
		if _, ok := fields[f.key]; !ok {
			continue
		}
		switch f.mode {
		case tagDrop:
			delete(fields, f.key)
		case tagRedact:
			fields[f.key] = json.RawMessage(`"` + redacted + `"`)
		}
	}
	state, err := json.Marshal(fields)
	require.NoError(t, err)
	return state
}

// filledRow returns an ent row with every column set to a non-zero value; edges stay unloaded as in a RETURNING write.
func filledRow(t *testing.T, name string) any {
	row := reflect.New(reflect.TypeOf(bareRow(t, name)).Elem())
	fill(row.Elem())
	return row.Interface()
}

func fill(v reflect.Value) {
	if !v.CanSet() {
		return
	}
	switch v.Kind() {
	case reflect.String:
		v.SetString("s_" + v.Type().Name())
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(7)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(7)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(1.5)
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		fill(v.Elem())
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key, val := reflect.New(v.Type().Key()).Elem(), reflect.New(v.Type().Elem()).Elem()
		fill(key)
		fill(val)
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Slice:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			v.SetBytes([]byte(`{}`))
			return
		}
		s := reflect.MakeSlice(v.Type(), 1, 1)
		fill(s.Index(0))
		v.Set(s)
	case reflect.Interface:
		v.Set(reflect.ValueOf("v"))
	case reflect.Struct:
		if v.Type() == reflect.TypeOf(time.Time{}) {
			v.Set(reflect.ValueOf(fixedTime))
			return
		}
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).Name != "Edges" {
				fill(v.Field(i))
			}
		}
	}
}

func TestSnapshot_MatchesJSONEncoding(t *testing.T) {
	reg := Registrations()
	for entityType := range entColumns {
		r, _ := reg.ByType(entityType)
		t.Run(string(entityType), func(t *testing.T) {
			for _, row := range []any{bareRow(t, r.EntName), filledRow(t, r.EntName)} {
				fields, ok := r.Snapshot.state(row)
				require.True(t, ok)
				state, err := json.Marshal(fields)
				require.NoError(t, err)
				require.JSONEq(t, string(encodedState(t, r, row)), string(state))
			}
		})
	}
}
