package types

import (
	"testing"
	"time"

	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
)

func TestParseYYYYMMDDToDate(t *testing.T) {
	tests := []struct {
		name           string
		input          *int
		expectedYear   int
		expectedMonth  time.Month
		expectedDay    int
		expectedHour   int
		expectedMinute int
		expectedSecond int
		expectedNano   int
		expectedZone   string
	}{
		{
			name:           "Valid date - beginning of year",
			input:          lo.ToPtr(20250101),
			expectedYear:   2025,
			expectedMonth:  time.January,
			expectedDay:    1,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Valid date - end of year",
			input:          lo.ToPtr(20251231),
			expectedYear:   2025,
			expectedMonth:  time.December,
			expectedDay:    31,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Valid date - middle of month",
			input:          lo.ToPtr(20250715),
			expectedYear:   2025,
			expectedMonth:  time.July,
			expectedDay:    15,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:  "Nil input",
			input: nil,
		},
		{
			name:           "Leap year - February 29",
			input:          lo.ToPtr(20240229),
			expectedYear:   2024,
			expectedMonth:  time.February,
			expectedDay:    29,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Non-leap year - February 28",
			input:          lo.ToPtr(20250228),
			expectedYear:   2025,
			expectedMonth:  time.February,
			expectedDay:    28,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Single digit month and day",
			input:          lo.ToPtr(20250101),
			expectedYear:   2025,
			expectedMonth:  time.January,
			expectedDay:    1,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Maximum valid date",
			input:          lo.ToPtr(99991231),
			expectedYear:   9999,
			expectedMonth:  time.December,
			expectedDay:    31,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Minimum valid date",
			input:          lo.ToPtr(10101),
			expectedYear:   1,
			expectedMonth:  time.January,
			expectedDay:    1,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Edge case - last day of different months",
			input:          lo.ToPtr(20250430),
			expectedYear:   2025,
			expectedMonth:  time.April,
			expectedDay:    30,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Edge case - first day after leap day",
			input:          lo.ToPtr(20240301),
			expectedYear:   2024,
			expectedMonth:  time.March,
			expectedDay:    1,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		// Invalid date cases - these will be normalized by time.Date
		{
			name:           "Invalid date - February 30 (normalized to March 2)",
			input:          lo.ToPtr(20250230),
			expectedYear:   2025,
			expectedMonth:  time.March,
			expectedDay:    2,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Invalid date - Month 13 (normalized to next year)",
			input:          lo.ToPtr(20251301),
			expectedYear:   2026,
			expectedMonth:  time.January,
			expectedDay:    1,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Invalid date - Day 32 (normalized to next month)",
			input:          lo.ToPtr(20250132),
			expectedYear:   2025,
			expectedMonth:  time.February,
			expectedDay:    1,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Edge case - Zero month (normalized)",
			input:          lo.ToPtr(20250001),
			expectedYear:   2024,
			expectedMonth:  time.December,
			expectedDay:    1,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Edge case - Zero day (normalized)",
			input:          lo.ToPtr(20250100),
			expectedYear:   2024,
			expectedMonth:  time.December,
			expectedDay:    31,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Edge case - All zeros except year",
			input:          lo.ToPtr(20250000),
			expectedYear:   2024,
			expectedMonth:  time.November,
			expectedDay:    30,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
		{
			name:           "Edge case - Negative year",
			input:          lo.ToPtr(-20250101),
			expectedYear:   -2026,
			expectedMonth:  time.October,
			expectedDay:    30,
			expectedHour:   0,
			expectedMinute: 0,
			expectedSecond: 0,
			expectedNano:   0,
			expectedZone:   "UTC",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseYYYYMMDDToDate(tt.input)

			if tt.input == nil {
				assert.Nil(t, result)
				return
			}

			assert.NotNil(t, result)

			year, month, day := result.Date()
			hour, min, sec := result.Clock()
			zone, _ := result.Zone()

			assert.Equal(t, tt.expectedYear, year, "Year mismatch")
			assert.Equal(t, tt.expectedMonth, month, "Month mismatch")
			assert.Equal(t, tt.expectedDay, day, "Day mismatch")
			assert.Equal(t, tt.expectedHour, hour, "Hour mismatch")
			assert.Equal(t, tt.expectedMinute, min, "Minute mismatch")
			assert.Equal(t, tt.expectedSecond, sec, "Second mismatch")
			assert.Equal(t, tt.expectedNano, result.Nanosecond(), "Nanosecond mismatch")
			assert.Equal(t, tt.expectedZone, zone, "Timezone mismatch")
		})
	}
}

// Additional test for checking specific behaviors
func TestParseYYYYMMDDToDate_SpecificBehaviors(t *testing.T) {
	t.Run("Verify UTC timezone", func(t *testing.T) {
		date := lo.ToPtr(20250101)
		result := ParseYYYYMMDDToDate(date)
		_, offset := result.Zone()
		assert.Equal(t, 0, offset, "Expected UTC timezone with 0 offset")
	})

	t.Run("Verify time components are zero", func(t *testing.T) {
		date := lo.ToPtr(20250101)
		result := ParseYYYYMMDDToDate(date)
		assert.Equal(t, 0, result.Hour(), "Hour should be 0")
		assert.Equal(t, 0, result.Minute(), "Minute should be 0")
		assert.Equal(t, 0, result.Second(), "Second should be 0")
		assert.Equal(t, 0, result.Nanosecond(), "Nanosecond should be 0")
	})

	t.Run("Verify date normalization", func(t *testing.T) {
		// Test with invalid date that should be normalized
		date := lo.ToPtr(20250229) // Not a leap year
		result := ParseYYYYMMDDToDate(date)

		// Should be normalized to March 1, 2025
		year, month, day := result.Date()
		assert.Equal(t, 2025, year)
		assert.Equal(t, time.March, month)
		assert.Equal(t, 1, day)
	})

	t.Run("Verify leap year handling", func(t *testing.T) {
		// Test multiple leap year cases
		leapYears := []int{2024, 2028, 2032, 2036, 2040}
		for _, year := range leapYears {
			date := lo.ToPtr(year*10000 + 229) // February 29
			result := ParseYYYYMMDDToDate(date)

			resultYear, month, day := result.Date()
			assert.Equal(t, year, resultYear)
			assert.Equal(t, time.February, month)
			assert.Equal(t, 29, day)
		}
	})
}

func TestEarliestOfAndLatestOf(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		a, b         time.Time
		wantEarliest time.Time
		wantLatest   time.Time
	}{
		{"a before b", early, late, early, late},
		{"b before a", late, early, early, late},
		{"equal", early, early, early, early},
		{"zero value participates", time.Time{}, early, time.Time{}, early},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EarliestOf(tt.a, tt.b); !got.Equal(tt.wantEarliest) {
				t.Errorf("EarliestOf(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.wantEarliest)
			}
			if got := LatestOf(tt.a, tt.b); !got.Equal(tt.wantLatest) {
				t.Errorf("LatestOf(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.wantLatest)
			}
		})
	}
}

func TestEarliestOfPtrAndLatestOfPtr(t *testing.T) {
	early := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	late := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name         string
		a, b         *time.Time
		wantEarliest *time.Time
		wantLatest   *time.Time
	}{
		{"both set", &early, &late, &early, &late},
		{"both set reversed", &late, &early, &early, &late},
		{"a nil falls back to b", nil, &late, &late, &late},
		{"b nil falls back to a", &early, nil, &early, &early},
		{"both nil", nil, nil, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotEarliest := EarliestOfPtr(tt.a, tt.b)
			if (gotEarliest == nil) != (tt.wantEarliest == nil) {
				t.Fatalf("EarliestOfPtr nil-ness = %v, want %v", gotEarliest, tt.wantEarliest)
			}
			if gotEarliest != nil && !gotEarliest.Equal(*tt.wantEarliest) {
				t.Errorf("EarliestOfPtr = %v, want %v", gotEarliest, tt.wantEarliest)
			}

			gotLatest := LatestOfPtr(tt.a, tt.b)
			if (gotLatest == nil) != (tt.wantLatest == nil) {
				t.Fatalf("LatestOfPtr nil-ness = %v, want %v", gotLatest, tt.wantLatest)
			}
			if gotLatest != nil && !gotLatest.Equal(*tt.wantLatest) {
				t.Errorf("LatestOfPtr = %v, want %v", gotLatest, tt.wantLatest)
			}
		})
	}
}
