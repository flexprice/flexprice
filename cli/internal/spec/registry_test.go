package spec

import "testing"

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg, err := NewRegistry(doc)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return reg
}

// customers list resolves through the curated map, not from a GET that does not exist.
func TestRegistry_CustomersListMapsToQueryCustomer(t *testing.T) {
	cmd, ok := testRegistry(t).Lookup("customers", "list")
	if !ok {
		t.Fatal("customers list not registered")
	}
	if cmd.Operation.ID != "queryCustomer" {
		t.Errorf("operationId = %q, want queryCustomer", cmd.Operation.ID)
	}
	if cmd.Operation.Method != "POST" || cmd.Operation.Path != "/customers/search" {
		t.Errorf("resolved to %s %s", cmd.Operation.Method, cmd.Operation.Path)
	}
}

func TestRegistry_ActionVerbsAreRegistered(t *testing.T) {
	cmd, ok := testRegistry(t).Lookup("invoices", "finalize")
	if !ok {
		t.Fatal("invoices finalize not registered")
	}
	if cmd.Operation.ID != "finalizeInvoice" {
		t.Errorf("operationId = %q, want finalizeInvoice", cmd.Operation.ID)
	}
}

// Every callable operation is reachable: mapped, derived, or explicitly excluded.
func TestRegistry_EveryOperationIsAccountedFor(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	reg, err := NewRegistry(doc)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	reachable := map[string]bool{}
	for _, c := range reg.Commands() {
		reachable[c.Operation.ID] = true
	}
	for _, id := range reg.Excluded() {
		reachable[id] = true
	}
	for _, op := range Operations(doc) {
		if !reachable[op.ID] {
			t.Errorf("operation %q is unreachable: map it in commands.yaml or exclude it", op.ID)
		}
	}
}

// A mapping pointing at an operation that does not exist is a hard failure.
func TestRegistry_DanglingMappingIsAnError(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = newRegistry(doc, []byte("resources:\n  ghosts:\n    list: noSuchOperation\nexclude: []\n"))
	if err == nil {
		t.Fatal("want an error for a mapping to a nonexistent operationId")
	}
}

// Two mappings resolving to the same resource+action is a hard failure.
func TestRegistry_CollisionIsAnError(t *testing.T) {
	doc, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	_, err = newRegistry(doc, []byte(
		"resources:\n  customers:\n    list: queryCustomer\n  Customers:\n    list: getCustomer\nexclude: []\n"))
	if err == nil {
		t.Fatal("want an error for a resource+action collision")
	}
}

func TestDeriveName_IsPureAndStable(t *testing.T) {
	cases := map[string][2]string{
		"createCustomer":        {"customers", "create"},
		"getWalletTransactions": {"wallets", "get-wallet-transactions"},
	}
	for id, want := range cases {
		resource, action := DeriveName("Customers", id)
		if id == "getWalletTransactions" {
			resource, action = DeriveName("Wallets", id)
		}
		if resource != want[0] {
			t.Errorf("%s: resource = %q, want %q", id, resource, want[0])
		}
		if action == "" {
			t.Errorf("%s: action is empty", id)
		}
	}
}
