package synthetic

import "testing"

func TestNewSDKClient_Constructs(t *testing.T) {
	c := NewSDKClient("https://api.example/v1", "k")
	if c == nil || c.Customers() == nil || c.Events() == nil || c.NewAsyncEventClient() == nil {
		t.Fatal("missing accessor")
	}
}
