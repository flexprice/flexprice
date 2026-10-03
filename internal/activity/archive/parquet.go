package archive

import (
	"io"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

// Row is one archived activity_logs record: every DDL column in DDL order, JSON and
// NULLs as strings. occurred_at keeps microseconds (parquet-go defaults to millis).
type Row struct {
	ID             string    `parquet:"id"`
	TenantID       string    `parquet:"tenant_id"`
	EnvironmentID  string    `parquet:"environment_id"`
	Category       string    `parquet:"category"`
	EntityType     string    `parquet:"entity_type"`
	EntityID       string    `parquet:"entity_id"`
	EntityLabel    string    `parquet:"entity_label"`
	Action         string    `parquet:"action"`
	ActorType      string    `parquet:"actor_type"`
	ActorID        string    `parquet:"actor_id"`
	ActorLabel     string    `parquet:"actor_label"`
	ActorUserID    string    `parquet:"actor_user_id,optional"`
	Source         string    `parquet:"source"`
	CustomerID     string    `parquet:"customer_id,optional"`
	SubscriptionID string    `parquet:"subscription_id,optional"`
	RequestID      string    `parquet:"request_id,optional"`
	Outcome        string    `parquet:"outcome"`
	ErrorCode      string    `parquet:"error_code,optional"`
	Changes        string    `parquet:"changes,optional"`
	Snapshot       string    `parquet:"snapshot,optional"`
	Metadata       string    `parquet:"metadata,optional"`
	OccurredAt     time.Time `parquet:"occurred_at,timestamp(microsecond)"`
}

type column struct {
	name     string
	optional bool
}

var rowColumns = func() []column {
	t := reflect.TypeOf(Row{})
	cols := make([]column, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		parts := strings.Split(t.Field(i).Tag.Get("parquet"), ",")
		cols = append(cols, column{name: parts[0], optional: slices.Contains(parts[1:], "optional")})
	}
	return cols
}()

// Columns returns the archived column names in Row field order.
func Columns() []string {
	out := make([]string, len(rowColumns))
	for i, c := range rowColumns {
		out[i] = c.name
	}
	return out
}

// SelectList returns SELECT expressions in Row field order, NULLs coalesced to empty strings.
func SelectList() string {
	exprs := make([]string, len(rowColumns))
	for i, c := range rowColumns {
		if c.optional {
			exprs[i] = "coalesce(" + c.name + "::text, '')"
		} else {
			exprs[i] = c.name
		}
	}
	return strings.Join(exprs, ", ")
}

// ScanDest returns pointers to r's fields in SelectList order, for rows.Scan.
func (r *Row) ScanDest() []any {
	v := reflect.ValueOf(r).Elem()
	out := make([]any, v.NumField())
	for i := range out {
		out[i] = v.Field(i).Addr().Interface()
	}
	return out
}

// WriteParquet writes rows as one zstd-compressed Parquet file.
func WriteParquet(w io.Writer, rows []Row) error {
	pw := parquet.NewGenericWriter[Row](w, parquet.Compression(&parquet.Zstd))
	if _, err := pw.Write(rows); err != nil {
		return err
	}
	return pw.Close()
}

// ReadParquet reads every row of a Parquet file written by WriteParquet.
func ReadParquet(r io.ReaderAt, size int64) ([]Row, error) {
	pr := parquet.NewGenericReader[Row](io.NewSectionReader(r, 0, size))
	defer pr.Close()
	out := make([]Row, pr.NumRows())
	n, err := pr.Read(out)
	if err != nil && err != io.EOF {
		return nil, err
	}
	return out[:n], nil
}
