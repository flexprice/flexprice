package inbox

import (
	"testing"
	"time"
)

func TestParse_ExtractsIdsFromNestedAndFlatPayloads(t *testing.T) {
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	alert := `{"event_type":"wallet.ongoing_balance.dropped","wallet":{"id":"wal_1"},"customer":{"id":"cust_1"},"alert":{"alert_type":"low_ongoing_balance","state":"in_alarm"}}`
	ev, err := Parse(at, []byte(alert))
	if err != nil {
		t.Fatal(err)
	}
	if ev.EventType != "wallet.ongoing_balance.dropped" || ev.WalletID != "wal_1" || ev.CustomerID != "cust_1" || ev.EntityID != "cust_1" || !ev.ReceivedAt.Equal(at) {
		t.Errorf("nested parse = %+v", ev)
	}
	if ev.Raw["alert"].(map[string]any)["state"] != "in_alarm" {
		t.Error("raw payload must be kept")
	}

	invoice := `{"event_type":"invoice.update.finalized","invoice_id":"inv_1","subscription_id":"sub_1","customer_id":"cust_2"}`
	ev, err = Parse(at, []byte(invoice))
	if err != nil {
		t.Fatal(err)
	}
	if ev.InvoiceID != "inv_1" || ev.SubscriptionID != "sub_1" || ev.CustomerID != "cust_2" {
		t.Errorf("flat parse = %+v", ev)
	}

	if _, err := Parse(at, []byte(`not json`)); err == nil {
		t.Error("invalid JSON must fail")
	}
	if _, err := Parse(at, []byte(`{"payload":{}}`)); err == nil {
		t.Error("an envelope without event_type must fail")
	}
}

func TestMatch_Matches(t *testing.T) {
	at := time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)
	ev := Event{ReceivedAt: at, EventType: "customer.created", CustomerID: "cust_1", WalletID: "wal_1", SubscriptionID: "sub_1"}
	cases := []struct {
		name string
		m    Match
		want bool
	}{
		{"empty matches all", Match{}, true},
		{"type", Match{EventType: "customer.created"}, true},
		{"other type", Match{EventType: "invoice.update"}, false},
		{"customer", Match{CustomerID: "cust_1"}, true},
		{"other customer", Match{CustomerID: "cust_2"}, false},
		{"wallet and sub", Match{WalletID: "wal_1", SubscriptionID: "sub_1"}, true},
		{"after earlier", Match{After: at.Add(-time.Second)}, true},
		{"after same instant", Match{After: at}, false},
	}
	for _, tc := range cases {
		if got := tc.m.Matches(ev); got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}
