package types

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestActiveWindows_Validate(t *testing.T) {
	window := func(sh, sm, eh, em int) ActiveWindow {
		return ActiveWindow{
			Start: Bucket{Hour: sh, Minute: sm},
			End:   Bucket{Hour: eh, Minute: em},
		}
	}

	tests := []struct {
		name    string
		windows ActiveWindows
		wantErr bool
	}{
		{"empty is all day", nil, false},
		{"single range", ActiveWindows{window(9, 0, 21, 0)}, false},
		{"midnight wrap", ActiveWindows{window(22, 0, 6, 0)}, false},
		{"whole day via end of day", ActiveWindows{window(0, 0, 24, 0)}, false},
		{"two disjoint ranges", ActiveWindows{window(9, 0, 12, 0), window(14, 0, 18, 0)}, false},
		{"negative hour", ActiveWindows{window(-1, 0, 9, 0)}, true},
		{"negative minute", ActiveWindows{window(0, -1, 9, 0)}, true},
		{"hour 25", ActiveWindows{window(25, 0, 10, 0)}, true},
		{"hour 24 with minutes", ActiveWindows{window(9, 0, 24, 30)}, true},
		{"minute 60", ActiveWindows{window(9, 60, 10, 0)}, true},
		{"start equals end", ActiveWindows{window(9, 0, 9, 0)}, true},
		{"overlapping ranges", ActiveWindows{window(9, 0, 12, 0), window(11, 0, 13, 0)}, true},
		{"wrap overlaps morning range", ActiveWindows{window(22, 0, 6, 0), window(5, 0, 7, 0)}, true},
		{"whole day overlaps any range", ActiveWindows{window(0, 0, 24, 0), window(5, 0, 7, 0)}, true},
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

	at := func(h, m int) time.Time {
		return time.Date(2026, 8, 25, h, m, 0, 0, ist).UTC()
	}

	w := ActiveWindows{
		{
			Start: Bucket{Hour: 9},
			End:   Bucket{Hour: 21},
		},
	}
	require.True(t, w.Contains(at(9, 0), ist))
	require.True(t, w.Contains(at(20, 59), ist))
	require.False(t, w.Contains(at(21, 0), ist))
	require.False(t, w.Contains(at(8, 59), ist))
	require.True(t, ActiveWindows(nil).Contains(at(3, 0), ist))

	wrap := ActiveWindows{
		{
			Start: Bucket{Hour: 22},
			End:   Bucket{Hour: 6},
		},
	}
	require.True(t, wrap.Contains(at(23, 30), ist))
	require.True(t, wrap.Contains(at(5, 59), ist))
	require.False(t, wrap.Contains(at(6, 0), ist))

	evening := ActiveWindows{
		{
			Start: Bucket{Hour: 18},
			End:   Bucket{Hour: 24},
		},
	}
	require.True(t, evening.Contains(at(23, 59), ist))
	require.False(t, evening.Contains(at(0, 0), ist))
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
	require.NoError(t, CommitmentCancellationFeePercentOfRemaining.Validate())
	require.Error(t, CommitmentCancellationFeeType("percent").Validate())
}
