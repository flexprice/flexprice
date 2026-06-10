package types

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestCommitmentType_Validate(t *testing.T) {
	tests := []struct {
		name string
		ct   CommitmentType
		want bool
	}{
		{"amount is valid", COMMITMENT_TYPE_AMOUNT, true},
		{"quantity is valid", COMMITMENT_TYPE_QUANTITY, true},
		{"unknown is invalid", CommitmentType("usage"), false},
		{"empty is invalid", CommitmentType(""), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.ct.Validate())
		})
	}
}

func TestCommitmentType_String(t *testing.T) {
	assert.Equal(t, "amount", COMMITMENT_TYPE_AMOUNT.String())
	assert.Equal(t, "quantity", COMMITMENT_TYPE_QUANTITY.String())
	assert.Equal(t, "", CommitmentType("").String())
}

func TestBucket_MinuteOfDay(t *testing.T) {
	tests := []struct {
		name   string
		bucket Bucket
		want   int
	}{
		{"midnight", Bucket{Hour: 0, Minute: 0}, 0},
		{"one minute past midnight", Bucket{Hour: 0, Minute: 1}, 1},
		{"9 AM", Bucket{Hour: 9, Minute: 0}, 540},
		{"5:30 PM", Bucket{Hour: 17, Minute: 30}, 1050},
		{"11:59 PM", Bucket{Hour: 23, Minute: 59}, 1439},
		{"end of day (24:00)", Bucket{Hour: 24, Minute: 0}, 1440},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.bucket.MinuteOfDay())
		})
	}
}

func TestTimeOfDayBucket_ContainsTime(t *testing.T) {
	// Helper: build a time at a specific UTC hour/minute.
	at := func(hour, minute int) time.Time {
		return time.Date(2026, time.June, 2, hour, minute, 0, 0, time.UTC)
	}

	tests := []struct {
		name   string
		bucket TimeOfDayBucket
		t      time.Time
		want   bool
	}{
		// Normal range [09:00, 17:00)
		{
			name:   "09:00-17:00: contains 09:00 (inclusive start)",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{17, 0}},
			t:      at(9, 0),
			want:   true,
		},
		{
			name:   "09:00-17:00: contains 13:00 (midpoint)",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{17, 0}},
			t:      at(13, 0),
			want:   true,
		},
		{
			name:   "09:00-17:00: excludes 17:00 (exclusive end)",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{17, 0}},
			t:      at(17, 0),
			want:   false,
		},
		{
			name:   "09:00-17:00: contains 16:59",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{17, 0}},
			t:      at(16, 59),
			want:   true,
		},
		{
			name:   "09:00-17:00: excludes 08:59",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{17, 0}},
			t:      at(8, 59),
			want:   false,
		},
		// Whole-day range [00:00, 24:00)
		{
			name:   "00:00-24:00: contains midnight",
			bucket: TimeOfDayBucket{Start: Bucket{0, 0}, End: Bucket{24, 0}},
			t:      at(0, 0),
			want:   true,
		},
		{
			name:   "00:00-24:00: contains 23:59",
			bucket: TimeOfDayBucket{Start: Bucket{0, 0}, End: Bucket{24, 0}},
			t:      at(23, 59),
			want:   true,
		},
		// Midnight-wrapping range [22:00, 06:00) covers 22:00..23:59 and 00:00..05:59
		{
			name:   "22:00-06:00: contains 22:00 (inclusive start)",
			bucket: TimeOfDayBucket{Start: Bucket{22, 0}, End: Bucket{6, 0}},
			t:      at(22, 0),
			want:   true,
		},
		{
			name:   "22:00-06:00: contains 23:59",
			bucket: TimeOfDayBucket{Start: Bucket{22, 0}, End: Bucket{6, 0}},
			t:      at(23, 59),
			want:   true,
		},
		{
			name:   "22:00-06:00: contains 00:00",
			bucket: TimeOfDayBucket{Start: Bucket{22, 0}, End: Bucket{6, 0}},
			t:      at(0, 0),
			want:   true,
		},
		{
			name:   "22:00-06:00: contains 05:59",
			bucket: TimeOfDayBucket{Start: Bucket{22, 0}, End: Bucket{6, 0}},
			t:      at(5, 59),
			want:   true,
		},
		{
			name:   "22:00-06:00: excludes 06:00 (exclusive end)",
			bucket: TimeOfDayBucket{Start: Bucket{22, 0}, End: Bucket{6, 0}},
			t:      at(6, 0),
			want:   false,
		},
		{
			name:   "22:00-06:00: excludes 12:00",
			bucket: TimeOfDayBucket{Start: Bucket{22, 0}, End: Bucket{6, 0}},
			t:      at(12, 0),
			want:   false,
		},
		// Empty range — start == end matches nothing
		{
			name:   "09:00-09:00: empty range excludes 09:00",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{9, 0}},
			t:      at(9, 0),
			want:   false,
		},
		{
			name:   "09:00-09:00: empty range excludes 12:00",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{9, 0}},
			t:      at(12, 0),
			want:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.bucket.ContainsTime(tt.t))
		})
	}
}

// ContainsTime must use the UTC hour/minute even when the input time is in a
// different zone. A 13:00 local time in IST is 07:30 UTC, which falls outside
// the [09:00, 17:00) UTC window.
func TestTimeOfDayBucket_ContainsTime_UsesUTC(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skip("Asia/Kolkata zone unavailable in test env")
	}

	bucket := TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{17, 0}}

	// 13:00 IST == 07:30 UTC → outside [09:00, 17:00) UTC
	localAfternoon := time.Date(2026, time.June, 2, 13, 0, 0, 0, ist)
	assert.False(t, bucket.ContainsTime(localAfternoon),
		"13:00 IST is 07:30 UTC and should fall outside [09:00, 17:00) UTC")

	// 15:00 IST == 09:30 UTC → inside [09:00, 17:00) UTC
	localBusinessHours := time.Date(2026, time.June, 2, 15, 0, 0, 0, ist)
	assert.True(t, bucket.ContainsTime(localBusinessHours),
		"15:00 IST is 09:30 UTC and should fall inside [09:00, 17:00) UTC")
}

func TestTimeOfDayBuckets_ContainsTime(t *testing.T) {
	at := func(hour, minute int) time.Time {
		return time.Date(2026, time.June, 2, hour, minute, 0, 0, time.UTC)
	}

	// Two-window setup: business hours and late evening.
	buckets := TimeOfDayBuckets{
		{Start: Bucket{9, 0}, End: Bucket{17, 0}},
		{Start: Bucket{20, 0}, End: Bucket{22, 0}},
	}

	tests := []struct {
		name string
		t    time.Time
		want bool
	}{
		{"inside first bucket", at(10, 0), true},
		{"inside second bucket", at(21, 0), true},
		{"between buckets", at(18, 0), false},
		{"before all buckets", at(8, 0), false},
		{"after all buckets", at(23, 0), false},
		{"on boundary of first bucket end (exclusive)", at(17, 0), false},
		{"on boundary of first bucket start (inclusive)", at(9, 0), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, buckets.ContainsTime(tt.t))
		})
	}
}

// An empty TimeOfDayBuckets slice should match nothing — callers use it as a
// sentinel meaning "no time-of-day restriction configured".
func TestTimeOfDayBuckets_ContainsTime_EmptySlice(t *testing.T) {
	var buckets TimeOfDayBuckets
	assert.False(t, buckets.ContainsTime(time.Date(2026, time.June, 2, 12, 0, 0, 0, time.UTC)))
}

func TestTimeOfDayBucket_Validate(t *testing.T) {
	posOne := decimal.NewFromInt(1)
	negOne := decimal.NewFromInt(-1)
	tests := []struct {
		name    string
		bucket  TimeOfDayBucket
		wantErr bool
		errSub  string
	}{
		{
			name: "valid amount commitment",
			bucket: TimeOfDayBucket{
				Start: Bucket{9, 0}, End: Bucket{10, 0},
				CommitmentType: COMMITMENT_TYPE_AMOUNT, CommitmentValue: decimal.NewFromInt(100),
				OverageFactor: &posOne,
			},
			wantErr: false,
		},
		{
			name: "start equals end",
			bucket: TimeOfDayBucket{
				Start: Bucket{9, 0}, End: Bucket{9, 0},
				CommitmentType: COMMITMENT_TYPE_AMOUNT, CommitmentValue: decimal.NewFromInt(1),
			},
			wantErr: true, errSub: "start must differ from end",
		},
		{
			name: "invalid commitment type",
			bucket: TimeOfDayBucket{
				Start: Bucket{9, 0}, End: Bucket{10, 0},
				CommitmentType: CommitmentType("bogus"), CommitmentValue: decimal.NewFromInt(1),
			},
			wantErr: true, errSub: "commitment_type",
		},
		{
			name: "negative commitment value",
			bucket: TimeOfDayBucket{
				Start: Bucket{9, 0}, End: Bucket{10, 0},
				CommitmentType: COMMITMENT_TYPE_AMOUNT, CommitmentValue: negOne,
			},
			wantErr: true, errSub: "commitment_value",
		},
		{
			name: "negative overage factor",
			bucket: TimeOfDayBucket{
				Start: Bucket{9, 0}, End: Bucket{10, 0},
				CommitmentType: COMMITMENT_TYPE_AMOUNT, CommitmentValue: decimal.NewFromInt(1),
				OverageFactor: &negOne,
			},
			wantErr: true, errSub: "overage_factor",
		},
		{
			// type set + zero value is a partial commitment shape; that check
			// governs here (it fires before the true-up check).
			name: "commitment type set without value",
			bucket: TimeOfDayBucket{
				Start: Bucket{9, 0}, End: Bucket{10, 0},
				CommitmentType: COMMITMENT_TYPE_AMOUNT, CommitmentValue: decimal.Zero,
				TrueUpEnabled: true,
			},
			wantErr: true, errSub: "commitment_value must be > 0",
		},
		{
			// true-up enabled with no type and no value still fails on the
			// true-up rule (no partial-shape branch applies).
			name: "true-up without commitment value",
			bucket: TimeOfDayBucket{
				Start: Bucket{9, 0}, End: Bucket{10, 0},
				TrueUpEnabled: true,
			},
			wantErr: true, errSub: "true_up_enabled",
		},
		{
			name: "midnight-wrapping valid",
			bucket: TimeOfDayBucket{
				Start: Bucket{22, 0}, End: Bucket{6, 0},
				CommitmentType: COMMITMENT_TYPE_AMOUNT, CommitmentValue: decimal.NewFromInt(1),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.bucket.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), tt.errSub)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTimeOfDayBucket_HasCommitment(t *testing.T) {
	tests := []struct {
		name   string
		bucket TimeOfDayBucket
		want   bool
	}{
		{
			name:   "no commitment value",
			bucket: TimeOfDayBucket{Start: Bucket{9, 0}, End: Bucket{10, 0}},
			want:   false,
		},
		{
			name: "amount commitment",
			bucket: TimeOfDayBucket{
				Start:           Bucket{9, 0},
				End:             Bucket{10, 0},
				CommitmentType:  COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.NewFromInt(100),
			},
			want: true,
		},
		{
			name: "quantity commitment",
			bucket: TimeOfDayBucket{
				Start:           Bucket{9, 0},
				End:             Bucket{10, 0},
				CommitmentType:  COMMITMENT_TYPE_QUANTITY,
				CommitmentValue: decimal.NewFromInt(1000),
			},
			want: true,
		},
		{
			name: "type set but value zero",
			bucket: TimeOfDayBucket{
				Start:           Bucket{9, 0},
				End:             Bucket{10, 0},
				CommitmentType:  COMMITMENT_TYPE_AMOUNT,
				CommitmentValue: decimal.Zero,
			},
			want: false,
		},
		{
			name: "type empty but value positive",
			bucket: TimeOfDayBucket{
				Start:           Bucket{9, 0},
				End:             Bucket{10, 0},
				CommitmentValue: decimal.NewFromInt(50),
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.bucket.HasCommitment())
		})
	}
}
