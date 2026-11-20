package e2eprobe

import "testing"

func TestErrorFromRawHTTPResponse_ClassifiesByStatus(t *testing.T) {
	cases := []struct {
		name  string
		code  int
		body  string
		infra bool
	}{
		{"plain 503", 503, "upstream down", true},
		{"plain 404", 404, "not found", false},
		{"typed 502", 502, `{"http_status_code":502,"message":"bad gateway"}`, true},
		{"typed 400", 400, `{"http_status_code":400,"message":"bad request"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := errorFromRawHTTPResponse(tc.code, []byte(tc.body))
			if err == nil || IsInfra(err) != tc.infra {
				t.Fatalf("err = %v, infra = %v, want %v", err, IsInfra(err), tc.infra)
			}
		})
	}
}
