package scheduledtask

import "testing"

func TestIsManagedCopy(t *testing.T) {
	tests := []struct {
		name string
		task *ScheduledTask
		want bool
	}{
		{"nil", nil, false},
		{"no connection", &ScheduledTask{}, true},
		{"with connection", &ScheduledTask{ConnectionID: "conn_1"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.task.IsManagedCopy(); got != tt.want {
				t.Fatalf("IsManagedCopy() = %v, want %v", got, tt.want)
			}
		})
	}
}
