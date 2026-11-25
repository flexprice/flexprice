package sample

import (
	"os"
	"runtime"
	"testing"
	"time"
)

func TestSleeps(t *testing.T) {
	time.Sleep(10 * time.Millisecond) // want `CL003: wait on the event, channel or clock`
}

func TestSkipWithoutReason(t *testing.T) {
	t.Skip() // want `CL003: say why the test is skipped`
}

func TestSkipUnconditionally(t *testing.T) {
	t.Skip("broken since the clock change") // want `CL003: an unconditional skip is a quarantined test`
}

func TestSkipQuarantined(t *testing.T) {
	// quarantine: FLE-1234 flaky until the clock is injected
	t.Skip("flaky until the clock is injected")
}

func TestSkipConditionally(t *testing.T) {
	if os.Getenv("FLEXPRICE_TEST_POSTGRES_DSN") == "" {
		t.Skip("set FLEXPRICE_TEST_POSTGRES_DSN to run PostgreSQL repository tests")
	}
}

func TestSkipInSwitchCase(t *testing.T) {
	switch runtime.GOOS {
	case "windows":
		t.Skip("requires Unix sockets")
	}
}

func TestLong(t *testing.T) { // want `CL003: TestLong runs 10[0-9] lines; turn the scenarios into table rows`
	n := 0
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	_ = n
}

func TestLongTable(t *testing.T) {
	cases := []struct {
		name string
		in   int
	}{
		{name: "one", in: 1},
		{name: "two", in: 2},
		{name: "three", in: 3},
		{name: "four", in: 4},
		{name: "five", in: 5},
		{name: "six", in: 6},
		{name: "seven", in: 7},
		{name: "eight", in: 8},
		{name: "nine", in: 9},
		{name: "ten", in: 10},
		{name: "eleven", in: 11},
		{name: "twelve", in: 12},
		{name: "thirteen", in: 13},
		{name: "fourteen", in: 14},
		{name: "fifteen", in: 15},
		{name: "sixteen", in: 16},
		{name: "seventeen", in: 17},
		{name: "eighteen", in: 18},
		{name: "nineteen", in: 19},
		{name: "twenty", in: 20},
		{name: "twenty-one", in: 21},
		{name: "twenty-two", in: 22},
		{name: "twenty-three", in: 23},
		{name: "twenty-four", in: 24},
		{name: "twenty-five", in: 25},
		{name: "twenty-six", in: 26},
		{name: "twenty-seven", in: 27},
		{name: "twenty-eight", in: 28},
		{name: "twenty-nine", in: 29},
		{name: "thirty", in: 30},
		{name: "thirty-one", in: 31},
		{name: "thirty-two", in: 32},
		{name: "thirty-three", in: 33},
		{name: "thirty-four", in: 34},
		{name: "thirty-five", in: 35},
		{name: "thirty-six", in: 36},
		{name: "thirty-seven", in: 37},
		{name: "thirty-eight", in: 38},
		{name: "thirty-nine", in: 39},
		{name: "forty", in: 40},
		{name: "forty-one", in: 41},
		{name: "forty-two", in: 42},
		{name: "forty-three", in: 43},
		{name: "forty-four", in: 44},
		{name: "forty-five", in: 45},
		{name: "forty-six", in: 46},
		{name: "forty-seven", in: 47},
		{name: "forty-eight", in: 48},
		{name: "forty-nine", in: 49},
		{name: "fifty", in: 50},
		{name: "fifty-one", in: 51},
		{name: "fifty-two", in: 52},
		{name: "fifty-three", in: 53},
		{name: "fifty-four", in: 54},
		{name: "fifty-five", in: 55},
		{name: "fifty-six", in: 56},
		{name: "fifty-seven", in: 57},
		{name: "fifty-eight", in: 58},
		{name: "fifty-nine", in: 59},
		{name: "sixty", in: 60},
		{name: "sixty-one", in: 61},
		{name: "sixty-two", in: 62},
		{name: "sixty-three", in: 63},
		{name: "sixty-four", in: 64},
		{name: "sixty-five", in: 65},
		{name: "sixty-six", in: 66},
		{name: "sixty-seven", in: 67},
		{name: "sixty-eight", in: 68},
		{name: "sixty-nine", in: 69},
		{name: "seventy", in: 70},
		{name: "seventy-one", in: 71},
		{name: "seventy-two", in: 72},
		{name: "seventy-three", in: 73},
		{name: "seventy-four", in: 74},
		{name: "seventy-five", in: 75},
		{name: "seventy-six", in: 76},
		{name: "seventy-seven", in: 77},
		{name: "seventy-eight", in: 78},
		{name: "seventy-nine", in: 79},
		{name: "eighty", in: 80},
		{name: "eighty-one", in: 81},
		{name: "eighty-two", in: 82},
		{name: "eighty-three", in: 83},
		{name: "eighty-four", in: 84},
		{name: "eighty-five", in: 85},
		{name: "eighty-six", in: 86},
		{name: "eighty-seven", in: 87},
		{name: "eighty-eight", in: 88},
		{name: "eighty-nine", in: 89},
		{name: "ninety", in: 90},
		{name: "ninety-one", in: 91},
		{name: "ninety-two", in: 92},
		{name: "ninety-three", in: 93},
		{name: "ninety-four", in: 94},
		{name: "ninety-five", in: 95},
		{name: "ninety-six", in: 96},
		{name: "ninety-seven", in: 97},
		{name: "ninety-eight", in: 98},
		{name: "ninety-nine", in: 99},
		{name: "hundred", in: 100},
		{name: "hundred-one", in: 101},
		{name: "hundred-two", in: 102},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.in == 0 {
				t.Fatal("zero")
			}
		})
	}
}
