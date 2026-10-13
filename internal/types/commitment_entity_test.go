package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestActiveWindows_Validate(t *testing.T) {
	tests := []struct {
		name    string
		windows ActiveWindows
		wantErr bool
	}{
		{"empty is all day", nil, false},
		{"single range", ActiveWindows{{Start: "09:00", End: "21:00"}}, false},
		{"midnight wrap", ActiveWindows{{Start: "22:00", End: "06:00"}}, false},
		{"two disjoint ranges", ActiveWindows{{Start: "09:00", End: "12:00"}, {Start: "14:00", End: "18:00"}}, false},
		{"bad format", ActiveWindows{{Start: "9", End: "21:00"}}, true},
		{"start equals end", ActiveWindows{{Start: "09:00", End: "09:00"}}, true},
		{"overlapping ranges", ActiveWindows{{Start: "09:00", End: "12:00"}, {Start: "11:00", End: "13:00"}}, true},
		{"wrap overlaps morning range", ActiveWindows{{Start: "22:00", End: "06:00"}, {Start: "05:00", End: "07:00"}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.windows.Validate()
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestActiveWindows_Contains(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	require.NoError(t, err)
	w := ActiveWindows{{Start: "09:00", End: "21:00"}}
	at := func(h, m int) time.Time { return time.Date(2026, 8, 25, h, m, 0, 0, ist).UTC() }

	require.True(t, w.Contains(at(9, 0), ist))
	require.True(t, w.Contains(at(20, 59), ist))
	require.False(t, w.Contains(at(21, 0), ist))
	require.False(t, w.Contains(at(8, 59), ist))
	require.True(t, ActiveWindows(nil).Contains(at(3, 0), ist))

	wrap := ActiveWindows{{Start: "22:00", End: "06:00"}}
	require.True(t, wrap.Contains(at(23, 30), ist))
	require.True(t, wrap.Contains(at(5, 59), ist))
	require.False(t, wrap.Contains(at(6, 0), ist))
}

func TestCommitmentEnums_Validate(t *testing.T) {
	require.NoError(t, CommitmentScopeGroup.Validate())
	require.Error(t, CommitmentScopeType("feature").Validate())
	require.NoError(t, CommitmentMeasurementWindow.Validate())
	require.Error(t, CommitmentMeasurement("term").Validate())
	require.NoError(t, CommitmentCollectionPerPeriod.Validate())
	require.Error(t, CommitmentCollectionMethod("x").Validate())
	require.NoError(t, CommitmentTerminationBillRemaining.Validate())
	require.Error(t, CommitmentEarlyTermination("refund").Validate())
}
