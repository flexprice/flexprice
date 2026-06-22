package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"strings"
)

// ============================================================================
// Configuration
// ============================================================================

const (
	oldCSVPath = "scripts/Vapi SKU Set up - vapi-pricing-02-feb.csv"
	newCSVPath = "scripts/control_public_billin_sku_price_component.csv"
	outputPath = "scripts/vapi-pricing-updated.csv"
)

// Aggregation fields that should be mapped to "billable_value" in our system.
// The client's sheet uses raw field names (characters, numCharacters, durationMS)
// but our import expects "billable_value" for all of these.
var billableFields = map[string]bool{
	"billable_value": true,
	"characters":     true,
	"numCharacters":  true,
	"durationMS":     true,
}

// Token-based aggregation fields - these are kept as-is.
var tokenFields = map[string]bool{
	"promptTokens":             true,
	"completionTokens":         true,
	"cachedPromptTokens":       true,
	"cacheReadInputTokens":     true,
	"cacheCreationInputTokens": true,
	// New voicemail-detect model token types
	"uncachedPromptTextTokens":  true,
	"uncachedPromptAudioTokens": true,
	"candidatesTextTokens":      true,
	"candidatesAudioTokens":     true,
	"cachedPromptTextTokens":    true,
	"cachedPromptAudioTokens":   true,
}

// maxDecimalPlaces is the max decimal precision allowed by the DB column numeric(25,15).
const maxDecimalPlaces = 15

// ============================================================================
// Types
// ============================================================================

// OldRow is a full row from the original pricing CSV (our import format).
type OldRow struct {
	FeatureName      string
	EventName        string
	AggregationType  string
	AggregationField string
	PricePerUnit     string
}

// TransformedNewRow is a row from the client's CSV after transformation.
type TransformedNewRow struct {
	DerivedFeatureName string // The feature_name we'd generate (used for matching)
	DerivedEventName   string
	AggregationType    string
	AggregationField   string
	PricePerUnit       string
}

// OutputRow is the final output format.
type OutputRow struct {
	FeatureName       string
	EventName         string
	AggregationType   string
	AggregationField  string
	PricePerUnit      string
	IsNew             bool // true = new feature not in old CSV
	HasPricingChanged bool // true = existed in old CSV but price changed in new CSV
}

// ============================================================================
// Main
// ============================================================================

func main() {
	fmt.Println("=== Pricing CSV Transformer (v2 - base sheet preserved) ===")
	fmt.Printf("Old CSV (base): %s\n", oldCSVPath)
	fmt.Printf("New CSV (updates): %s\n", newCSVPath)
	fmt.Printf("Output: %s\n\n", outputPath)

	// Step 1: Read old CSV into ordered list + lookup map
	oldRows, oldRowMap := readOldCSV(oldCSVPath)
	fmt.Printf("[1/4] Read %d rows from old CSV (base sheet)\n", len(oldRows))

	// Step 2: Read new CSV, transform, and build lookup map
	newRowMap := readAndTransformNewCSV(newCSVPath)
	fmt.Printf("[2/4] Read %d unique transformed rows from new CSV\n", len(newRowMap))

	// Step 3: Build output by merging old (base) + new (updates/additions)
	outputRows := buildOutput(oldRows, oldRowMap, newRowMap)
	fmt.Printf("[3/4] Generated %d output rows\n", len(outputRows))

	// Step 4: Write output
	writeOutputCSV(outputPath, outputRows)
	fmt.Printf("[4/4] Output written to %s\n", outputPath)

	// Summary
	printSummary(outputRows)
}

// ============================================================================
// Step 1: Read Old CSV (the base sheet)
// ============================================================================

// readOldCSV reads the original pricing CSV (with header) and returns:
// - An ordered slice of all rows (to preserve original ordering)
// - A map of feature_name -> OldRow (for quick lookup)
func readOldCSV(path string) ([]OldRow, map[string]OldRow) {
	file, err := os.Open(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Cannot open old CSV %s: %v\n", path, err)
		os.Exit(1)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// Read and skip header
	header, err := reader.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Cannot read old CSV header: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  Old CSV header: %v\n", header)

	var rows []OldRow
	rowMap := make(map[string]OldRow)

	for {
		record, err := reader.Read()
		if err != nil {
			break
		}
		if len(record) < 5 {
			continue
		}

		row := OldRow{
			FeatureName:      strings.TrimSpace(record[0]),
			EventName:        strings.TrimSpace(record[1]),
			AggregationType:  strings.TrimSpace(record[2]),
			AggregationField: strings.TrimSpace(record[3]),
			PricePerUnit:     strings.TrimSpace(record[4]),
		}
		rows = append(rows, row)
		rowMap[row.FeatureName] = row
	}

	return rows, rowMap
}

// ============================================================================
// Step 2: Read & Transform New CSV
// ============================================================================

// RawNewRow represents a single row from the client's CSV (3 columns, no header).
type RawNewRow struct {
	FeatureName  string
	AggField     string
	PricePerUnit string
}

// readAndTransformNewCSV reads the client's CSV, transforms each row, and returns
// a map of derived_feature_name -> TransformedNewRow. Duplicates are resolved by
// keeping the last occurrence.
func readAndTransformNewCSV(path string) map[string]TransformedNewRow {
	rawRows := readNewCSVRaw(path)
	fmt.Printf("  Parsed %d raw rows from new CSV\n", len(rawRows))

	result := make(map[string]TransformedNewRow)
	duplicateCount := 0

	for _, raw := range rawRows {
		transformed := transformNewRow(raw)

		if existing, exists := result[transformed.DerivedFeatureName]; exists {
			if existing.PricePerUnit != transformed.PricePerUnit {
				fmt.Printf("  DEDUP (price conflict): '%s' prices %s vs %s -> keeping %s\n",
					transformed.DerivedFeatureName, existing.PricePerUnit, transformed.PricePerUnit, transformed.PricePerUnit)
			}
			duplicateCount++
		}
		result[transformed.DerivedFeatureName] = transformed
	}

	if duplicateCount > 0 {
		fmt.Printf("  Resolved %d duplicate(s) in new CSV\n", duplicateCount)
	}

	return result
}

// readNewCSVRaw reads the client's CSV file (no header, 3 columns) with corruption handling.
func readNewCSVRaw(path string) []RawNewRow {
	rawBytes, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Cannot read new CSV %s: %v\n", path, err)
		os.Exit(1)
	}

	content := string(rawBytes)

	// Fix known data corruption: missing newline between two rows on line ~1100
	// "vapi-audio-recording,durationMS,011labs-synthesizer-eleven_turbo_v2_5,durationMS,0.00005"
	// should be two separate rows.
	content = strings.Replace(content,
		"vapi-audio-recording,durationMS,011labs-synthesizer-eleven_turbo_v2_5,durationMS,0.00005",
		"vapi-audio-recording,durationMS,0\n11labs-synthesizer-eleven_turbo_v2_5,durationMS,0.00005",
		1)

	var rows []RawNewRow
	scanner := bufio.NewScanner(strings.NewReader(content))
	lineNum := 0
	skipped := 0

	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) == 3 {
			rows = append(rows, RawNewRow{
				FeatureName:  strings.TrimSpace(fields[0]),
				AggField:     strings.TrimSpace(fields[1]),
				PricePerUnit: strings.TrimSpace(fields[2]),
			})
		} else {
			fmt.Fprintf(os.Stderr, "  WARNING: Line %d has %d fields (expected 3), skipping: %s\n",
				lineNum, len(fields), truncate(line, 100))
			skipped++
		}
	}

	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "  Skipped %d corrupted lines\n", skipped)
	}

	return rows
}

// transformNewRow applies naming/aggregation transformations to a raw new CSV row.
func transformNewRow(raw RawNewRow) TransformedNewRow {
	targetAggField := raw.AggField
	isBillableType := billableFields[raw.AggField]
	isTokenType := tokenFields[raw.AggField]

	// Map characters/numCharacters/durationMS -> billable_value
	if isBillableType {
		targetAggField = "billable_value"
	}

	var featureName, eventName string

	if isBillableType {
		// For billable types: strip agg field suffix, feature_name = event_name
		featureName = stripAggFieldSuffix(raw.FeatureName, raw.AggField)
		eventName = featureName
	} else if isTokenType {
		// For token types: feature_name keeps suffix, event_name strips it
		featureName = raw.FeatureName
		eventName = stripAggFieldSuffix(raw.FeatureName, raw.AggField)
	} else {
		// Unknown aggregation field - treat like token type (keep suffix in name)
		featureName = raw.FeatureName
		eventName = stripAggFieldSuffix(raw.FeatureName, raw.AggField)
	}

	// Remove any trailing hyphens
	featureName = strings.TrimRight(featureName, "-")
	eventName = strings.TrimRight(eventName, "-")

	return TransformedNewRow{
		DerivedFeatureName: featureName,
		DerivedEventName:   eventName,
		AggregationType:    "SUM",
		AggregationField:   targetAggField,
		PricePerUnit:       raw.PricePerUnit,
	}
}

// ============================================================================
// Step 3: Build Output (merge old base + new updates/additions)
// ============================================================================

// buildOutput creates the final output by:
// 1. Starting with ALL rows from the old CSV (preserving feature_name exactly)
// 2. For rows that also appear in new CSV: update price_per_unit from new CSV
// 3. Appending rows from new CSV that don't exist in old CSV (new features)
func buildOutput(oldRows []OldRow, oldRowMap map[string]OldRow, newRowMap map[string]TransformedNewRow) []OutputRow {
	var output []OutputRow
	matchedNewKeys := make(map[string]bool) // Track which new rows were matched to old rows
	roundedCount := 0

	// --- Part A: Process all old rows (the base) ---
	for _, old := range oldRows {
		// Round the base price to fit DB precision
		price := old.PricePerUnit
		roundedPrice, wasRounded := roundPrice(price)
		if wasRounded {
			fmt.Printf("  ROUND (base): '%s' price %s -> %s\n", old.FeatureName, price, roundedPrice)
			roundedCount++
		}

		outRow := OutputRow{
			FeatureName:       old.FeatureName,      // PRESERVED exactly
			EventName:         old.EventName,        // PRESERVED exactly
			AggregationType:   old.AggregationType,  // PRESERVED exactly
			AggregationField:  old.AggregationField, // PRESERVED exactly
			PricePerUnit:      roundedPrice,         // Rounded to fit DB
			IsNew:             false,                // Exists in old CSV
			HasPricingChanged: false,                // Default: no change
		}

		// Check if there's a matching entry in the new CSV
		if newRow, found := newRowMap[old.FeatureName]; found {
			matchedNewKeys[old.FeatureName] = true

			// Round the new price too
			newPrice, newWasRounded := roundPrice(newRow.PricePerUnit)
			if newWasRounded {
				fmt.Printf("  ROUND (update): '%s' new price %s -> %s\n", old.FeatureName, newRow.PricePerUnit, newPrice)
				roundedCount++
			}

			// Rule 4: For price conflicts, pick the new sheet's price (after rounding)
			if normalizePriceForCompare(roundedPrice) != normalizePriceForCompare(newPrice) {
				outRow.PricePerUnit = newPrice
				outRow.HasPricingChanged = true
			}
		}

		output = append(output, outRow)
	}

	// --- Part B: Add rows from new CSV that don't exist in old CSV ---
	var newOnlyRows []TransformedNewRow
	for derivedName, newRow := range newRowMap {
		if !matchedNewKeys[derivedName] {
			// This feature doesn't exist in the old CSV -> it's truly new
			newOnlyRows = append(newOnlyRows, newRow)
		}
	}

	for _, newRow := range newOnlyRows {
		// Round price for new rows too
		price, wasRounded := roundPrice(newRow.PricePerUnit)
		if wasRounded {
			fmt.Printf("  ROUND (new): '%s' price %s -> %s\n", newRow.DerivedFeatureName, newRow.PricePerUnit, price)
			roundedCount++
		}

		output = append(output, OutputRow{
			FeatureName:       newRow.DerivedFeatureName,
			EventName:         newRow.DerivedEventName,
			AggregationType:   newRow.AggregationType,
			AggregationField:  newRow.AggregationField,
			PricePerUnit:      price,
			IsNew:             true,
			HasPricingChanged: false, // N/A for new features
		})
	}

	if roundedCount > 0 {
		fmt.Printf("  Rounded %d price(s) to fit DB precision (max %d decimal places)\n", roundedCount, maxDecimalPlaces)
	}

	return output
}

// normalizePriceForCompare normalizes price strings for comparison.
// Handles cases like "0" vs "0.00000000" or trailing zeros.
func normalizePriceForCompare(price string) string {
	// Parse as float and format consistently to handle trailing zeros, etc.
	// Use string manipulation to avoid floating point precision issues with very small numbers.
	// Trim trailing zeros after decimal point for comparison.
	p := strings.TrimRight(price, "0")
	p = strings.TrimRight(p, ".")
	if p == "" || p == "-" {
		return "0"
	}
	return p
}

// ============================================================================
// Step 4: Write Output CSV
// ============================================================================

func writeOutputCSV(path string, rows []OutputRow) {
	file, err := os.Create(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Cannot create output CSV %s: %v\n", path, err)
		os.Exit(1)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write header
	if err := writer.Write([]string{
		"feature_name", "event_name", "aggregation_type",
		"aggregation_field", "price_per_unit", "is_new", "has_pricing_changed",
	}); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Failed to write header: %v\n", err)
		os.Exit(1)
	}

	for _, row := range rows {
		isNewStr := "false"
		if row.IsNew {
			isNewStr = "true"
		}
		hasPricingChangedStr := "false"
		if row.HasPricingChanged {
			hasPricingChangedStr = "true"
		}
		if err := writer.Write([]string{
			row.FeatureName,
			row.EventName,
			row.AggregationType,
			row.AggregationField,
			row.PricePerUnit,
			isNewStr,
			hasPricingChangedStr,
		}); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: Failed to write row: %v\n", err)
		}
	}
}

// ============================================================================
// Summary
// ============================================================================

func printSummary(rows []OutputRow) {
	totalRows := len(rows)
	existingUnchanged := 0
	existingPriceChanged := 0
	newFeatures := 0

	pricingChanges := make([]OutputRow, 0)
	newFeatureNames := make([]string, 0)

	for _, row := range rows {
		switch {
		case row.IsNew:
			newFeatures++
			newFeatureNames = append(newFeatureNames, row.FeatureName)
		case row.HasPricingChanged:
			existingPriceChanged++
			pricingChanges = append(pricingChanges, row)
		default:
			existingUnchanged++
		}
	}

	fmt.Println("\n====================================================")
	fmt.Println("                   IMPORT SUMMARY                   ")
	fmt.Println("====================================================")
	fmt.Printf("Total output rows:                %d\n", totalRows)
	fmt.Println("----------------------------------------------------")
	fmt.Printf("Existing (unchanged):             %d  (is_new=false, has_pricing_changed=false)\n", existingUnchanged)
	fmt.Printf("Existing (price updated):         %d  (is_new=false, has_pricing_changed=true)\n", existingPriceChanged)
	fmt.Printf("New features:                     %d  (is_new=true)\n", newFeatures)
	fmt.Println("----------------------------------------------------")

	// Show pricing changes
	if len(pricingChanges) > 0 {
		fmt.Println("\nPricing changes (existing features with updated prices):")
		limit := 30
		if len(pricingChanges) < limit {
			limit = len(pricingChanges)
		}
		for i := 0; i < limit; i++ {
			fmt.Printf("  - %s\n", pricingChanges[i].FeatureName)
		}
		if len(pricingChanges) > 30 {
			fmt.Printf("  ... and %d more\n", len(pricingChanges)-30)
		}
	}

	// Show new features (sample)
	if len(newFeatureNames) > 0 {
		fmt.Println("\nNew features (sample, first 20):")
		limit := 20
		if len(newFeatureNames) < limit {
			limit = len(newFeatureNames)
		}
		for i := 0; i < limit; i++ {
			fmt.Printf("  - %s\n", newFeatureNames[i])
		}
		if len(newFeatureNames) > 20 {
			fmt.Printf("  ... and %d more\n", len(newFeatureNames)-20)
		}
	}

	fmt.Println("====================================================")
}

// ============================================================================
// Utilities
// ============================================================================

// stripAggFieldSuffix removes the "-{aggField}" suffix from a feature name if present.
func stripAggFieldSuffix(featureName, aggField string) string {
	suffix := "-" + aggField
	if strings.HasSuffix(featureName, suffix) {
		return featureName[:len(featureName)-len(suffix)]
	}
	return featureName
}

// roundPrice rounds a price string to maxDecimalPlaces (15) decimal places
// to fit within the DB column numeric(25,15). Uses pure string arithmetic
// to avoid float64 precision loss on very small numbers.
//
// Examples:
//
//	"0.0000006249999999999999" -> "0.000000625"       (rounded)
//	"0.00000026666666666666667"-> "0.000000266666667" (rounded)
//	"0.000015"                 -> "0.000015"          (unchanged, already fits)
func roundPrice(priceStr string) (string, bool) {
	// Handle negative sign
	negative := false
	s := priceStr
	if strings.HasPrefix(s, "-") {
		negative = true
		s = s[1:]
	}

	parts := strings.SplitN(s, ".", 2)
	intPart := parts[0]
	if intPart == "" {
		intPart = "0"
	}

	// No decimal part or fits within precision -> no rounding needed
	if len(parts) < 2 || len(parts[1]) <= maxDecimalPlaces {
		return priceStr, false
	}

	fracPart := parts[1]

	// Look at the digit right after maxDecimalPlaces to decide rounding
	roundDigit := fracPart[maxDecimalPlaces] - '0'
	truncated := []byte(fracPart[:maxDecimalPlaces])

	if roundDigit >= 5 {
		// Round up: add 1 to the last truncated digit, propagate carry
		carry := true
		for i := len(truncated) - 1; i >= 0 && carry; i-- {
			d := truncated[i] - '0' + 1
			if d >= 10 {
				truncated[i] = '0'
			} else {
				truncated[i] = byte('0' + d)
				carry = false
			}
		}
		if carry {
			// Carry overflows into the integer part
			intPart = incrementIntStr(intPart)
		}
	}

	// Build result, trim trailing zeros for clean output
	result := intPart + "." + string(truncated)
	result = strings.TrimRight(result, "0")
	result = strings.TrimRight(result, ".")
	if result == "" {
		result = "0"
	}
	if negative && result != "0" {
		result = "-" + result
	}

	return result, true
}

// incrementIntStr adds 1 to a non-negative integer represented as a string.
func incrementIntStr(s string) string {
	if s == "" || s == "0" {
		return "1"
	}
	digits := []byte(s)
	carry := true
	for i := len(digits) - 1; i >= 0 && carry; i-- {
		d := digits[i] - '0' + 1
		if d >= 10 {
			digits[i] = '0'
		} else {
			digits[i] = byte('0' + d)
			carry = false
		}
	}
	result := string(digits)
	if carry {
		result = "1" + result
	}
	return result
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
