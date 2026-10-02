package archive

import (
	"bytes"
	"reflect"
	"testing"
	"time"
)

func TestParquetRoundTrip(t *testing.T) {
	in := []Row{
		{
			ID: "act_1", TenantID: "t", EnvironmentID: "e", Category: "business",
			EntityType: "customer", EntityID: "c", EntityLabel: "Acme",
			Action: "customer.created", ActorType: "user", ActorID: "u", ActorLabel: "a@b.c", ActorUserID: "u",
			Source: "api", CustomerID: "c", SubscriptionID: "sub_1", RequestID: "req_1",
			Outcome: "success", ErrorCode: "", Changes: `{"a":{"from":1,"to":2}}`,
			Snapshot: `{"name":"Acme"}`, Metadata: `{"k":"v"}`,
			OccurredAt: time.Date(2026, 5, 1, 2, 3, 4, 0, time.UTC),
		},
		{
			ID: "act_2", TenantID: "t", EnvironmentID: "e", Category: "business",
			EntityType: "customer", EntityID: "c", Action: "customer.updated",
			ActorType: "system", ActorID: "system", Source: "worker", Outcome: "failure", ErrorCode: "boom",
			OccurredAt: time.Date(2026, 5, 2, 0, 0, 0, 123000, time.UTC),
		},
	}
	var buf bytes.Buffer
	if err := WriteParquet(&buf, in); err != nil {
		t.Fatal(err)
	}
	out, err := ReadParquet(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != len(in) {
		t.Fatalf("want %d rows, got %d", len(in), len(out))
	}
	for i := range in {
		if !out[i].OccurredAt.Equal(in[i].OccurredAt) {
			t.Fatalf("row %d occurred_at: want %v got %v", i, in[i].OccurredAt, out[i].OccurredAt)
		}
		out[i].OccurredAt = in[i].OccurredAt
		if !reflect.DeepEqual(out[i], in[i]) {
			t.Fatalf("row %d round trip mismatch:\nwant %+v\ngot  %+v", i, in[i], out[i])
		}
	}
}

// TestRowCarriesEveryColumn pins the Parquet schema to the activity_logs DDL:
// an archive missing a column cannot be restored.
func TestRowCarriesEveryColumn(t *testing.T) {
	want := []string{
		"id", "tenant_id", "environment_id", "category", "entity_type", "entity_id", "entity_label",
		"action", "actor_type", "actor_id", "actor_label", "actor_user_id", "source", "customer_id",
		"subscription_id", "request_id", "outcome", "error_code", "changes", "snapshot", "metadata", "occurred_at",
	}
	got := Columns()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("columns:\nwant %v\ngot  %v", want, got)
	}
}

func TestSelectListAndScanDestAlign(t *testing.T) {
	want := "id, tenant_id, environment_id, category, entity_type, entity_id, entity_label, action, actor_type, actor_id, actor_label, " +
		"coalesce(actor_user_id::text, ''), source, coalesce(customer_id::text, ''), coalesce(subscription_id::text, ''), " +
		"coalesce(request_id::text, ''), outcome, coalesce(error_code::text, ''), coalesce(changes::text, ''), " +
		"coalesce(snapshot::text, ''), coalesce(metadata::text, ''), occurred_at"
	if got := SelectList(); got != want {
		t.Fatalf("select list:\nwant %s\ngot  %s", want, got)
	}
	var r Row
	dest := r.ScanDest()
	if len(dest) != len(Columns()) {
		t.Fatalf("want %d scan targets, got %d", len(Columns()), len(dest))
	}
	*(dest[6].(*string)) = "label"
	*(dest[14].(*string)) = "sub_1"
	*(dest[21].(*time.Time)) = time.Unix(1, 0)
	if r.EntityLabel != "label" || r.SubscriptionID != "sub_1" || !r.OccurredAt.Equal(time.Unix(1, 0)) {
		t.Fatalf("scan targets misaligned: %+v", r)
	}
}
