package incident

import (
	"fmt"
	"sort"
	"strings"
)

// FormatDigest renders the periodic summary for Slack and the log.
func FormatDigest(d Digest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "e2eprobe digest · %s\n", d.GeneratedAt.UTC().Format("2006-01-02 15:04 UTC"))
	runs, passes := 0, 0
	var skipping []BindingState
	for _, st := range d.Bindings {
		runs += st.Runs
		passes += st.Passes
		if !st.SkippingSince.IsZero() {
			skipping = append(skipping, st)
		}
	}
	fmt.Fprintf(&b, "%d bindings · %d runs · %d passed\n", len(d.Bindings), runs, passes)

	fmt.Fprintf(&b, "Open incidents (%d)\n", len(d.Open))
	for _, inc := range d.Open {
		fmt.Fprintf(&b, "• %s · %s · open for %s · %d runs · %s\n", inc.Key, inc.Kind, humanDuration(d.GeneratedAt.Sub(inc.OpenedAt)), inc.Runs, inc.Summary)
	}
	fmt.Fprintf(&b, "Skipping (%d)\n", len(skipping))
	for _, st := range skipping {
		fmt.Fprintf(&b, "• %s · %s · %s\n", st.Key, humanDuration(d.GeneratedAt.Sub(st.SkippingSince)), st.Last.Reason)
	}
	keys := make([]string, 0, len(d.Extra))
	for k := range d.Extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s: %s\n", k, d.Extra[k])
	}
	return strings.TrimRight(b.String(), "\n")
}
