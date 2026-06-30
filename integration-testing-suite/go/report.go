package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"os"
	"strings"
	"time"
)

// generateHTMLReport writes a self-contained HTML report to the given path.
// It correlates StepResults with CallRecords from the TrafficLogger.
func generateHTMLReport(
	results []StepResult,
	calls []CallRecord,
	totalDuration time.Duration,
	reportPath string,
) error {
	// Index calls by step number.
	callsByStep := make(map[int][]CallRecord)
	for _, c := range calls {
		callsByStep[c.StepNumber] = append(callsByStep[c.StepNumber], c)
	}

	// Count outcomes.
	var passed, failed, skipped int
	for _, r := range results {
		switch {
		case r.Skipped:
			skipped++
		case r.Passed:
			passed++
		default:
			failed++
		}
	}

	// Build pinning timeline: steps where a write occurred (PinUntilMs > 0 in any call)
	// or where a read was pinned (WriterPinned > 0).
	type pinEvent struct {
		StepNumber   int
		StepName     string
		StepPassed   bool
		WriteOccurred bool   // any call in this step had PinUntilMs > 0
		PinSent      bool   // any call sent X-Pin-To-Writer: true
		Reader       int
		WriterPinned int
		WriterCalls  int
		URLs         []string
	}
	var pinTimeline []pinEvent

	for _, r := range results {
		if r.Skipped {
			continue
		}
		sc := callsByStep[r.StepNumber]
		var writeOccurred, pinSent bool
		var maxReader, maxWP, maxWC int
		var urls []string
		for _, c := range sc {
			if c.PinUntilMs > 0 {
				writeOccurred = true
			}
			if c.PinSent {
				pinSent = true
			}
			if c.Routing.Reader > maxReader {
				maxReader = c.Routing.Reader
			}
			if c.Routing.WriterPinned > maxWP {
				maxWP = c.Routing.WriterPinned
			}
			if c.Routing.WriterCalls > maxWC {
				maxWC = c.Routing.WriterCalls
			}
			// Shorten URL for display
			u := c.URL
			if idx := strings.Index(u, "/v1/"); idx >= 0 {
				u = c.Method + " " + u[idx:]
			} else {
				u = c.Method + " " + u
			}
			urls = append(urls, u)
		}
		if writeOccurred || pinSent || maxWP > 0 || maxWC > 0 || maxReader > 0 {
			pinTimeline = append(pinTimeline, pinEvent{
				StepNumber:   r.StepNumber,
				StepName:     r.StepName,
				StepPassed:   r.Passed,
				WriteOccurred: writeOccurred,
				PinSent:      pinSent,
				Reader:       maxReader,
				WriterPinned: maxWP,
				WriterCalls:  maxWC,
				URLs:         urls,
			})
		}
	}

	var buf bytes.Buffer
	w := &buf

	// ── HTML header ──────────────────────────────────────────────────────────
	fmt.Fprintf(w, `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>FlexPrice Integration Test Report — %s</title>
<style>
* { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #0f1117; color: #e2e8f0; line-height: 1.5; }
h1 { font-size: 1.4rem; font-weight: 700; }
h2 { font-size: 1.1rem; font-weight: 600; margin-bottom: .75rem; color: #94a3b8; text-transform: uppercase; letter-spacing: .05em; }
h3 { font-size: .9rem; font-weight: 600; }
.container { max-width: 1200px; margin: 0 auto; padding: 1.5rem; }

/* summary card */
.summary-card { background: #1e2330; border-radius: 10px; padding: 1.25rem 1.5rem; margin-bottom: 1.5rem; display: flex; gap: 2rem; flex-wrap: wrap; align-items: center; }
.summary-card .title { flex: 1; min-width: 200px; }
.summary-card .title small { color: #64748b; font-size: .8rem; }
.stat { text-align: center; }
.stat .num { font-size: 2rem; font-weight: 700; }
.stat .label { font-size: .75rem; color: #64748b; text-transform: uppercase; letter-spacing: .05em; }
.pass-num { color: #4ade80; }
.fail-num { color: #f87171; }
.skip-num { color: #94a3b8; }

/* routing analysis */
.routing-section { background: #1e2330; border-radius: 10px; padding: 1.25rem 1.5rem; margin-bottom: 1.5rem; }
table { width: 100%; border-collapse: collapse; font-size: .82rem; }
th { background: #0f1117; color: #64748b; text-transform: uppercase; font-size: .7rem; letter-spacing: .05em; padding: .5rem .75rem; text-align: left; border-bottom: 1px solid #2d3748; }
td { padding: .45rem .75rem; border-bottom: 1px solid #1a2030; vertical-align: top; }
tr:last-child td { border-bottom: none; }
tr:hover td { background: #0f1117; }
.badge { display: inline-block; padding: .1rem .45rem; border-radius: 4px; font-size: .7rem; font-weight: 600; }
.badge-write { background: #7c3aed33; color: #c4b5fd; border: 1px solid #7c3aed55; }
.badge-pin-sent { background: #1d4ed833; color: #93c5fd; border: 1px solid #1d4ed855; }
.badge-read { background: #06513333; color: #6ee7b7; border: 1px solid #06513355; }
.badge-pass { background: #16543033; color: #4ade80; border: 1px solid #16543055; }
.badge-fail { background: #7f1d1d33; color: #f87171; border: 1px solid #7f1d1d55; }
.badge-skip { background: #1e293b; color: #94a3b8; border: 1px solid #334155; }
.num-green { color: #4ade80; font-weight: 600; }
.num-blue { color: #93c5fd; font-weight: 600; }
.num-yellow { color: #fbbf24; font-weight: 600; }
.num-gray { color: #475569; }

/* steps */
.steps-section { margin-bottom: 1.5rem; }
.step-item { background: #1e2330; border-radius: 8px; margin-bottom: .5rem; overflow: hidden; }
details > summary { list-style: none; }
details > summary::-webkit-details-marker { display: none; }
.step-header { display: flex; align-items: center; gap: .75rem; padding: .75rem 1rem; cursor: pointer; user-select: none; }
.step-header:hover { background: #252d3f; }
.step-num { color: #475569; font-size: .8rem; width: 2rem; text-align: right; flex-shrink: 0; }
.step-status { font-size: .75rem; font-weight: 700; width: 3.5rem; flex-shrink: 0; }
.status-pass { color: #4ade80; }
.status-fail { color: #f87171; }
.status-skip { color: #94a3b8; }
.step-name { flex: 1; font-size: .88rem; }
.step-meta { display: flex; gap: .5rem; align-items: center; flex-shrink: 0; }
.step-dur { color: #64748b; font-size: .78rem; }
.step-err { color: #fca5a5; font-size: .78rem; padding: .25rem .75rem 0 4.75rem; }
.step-details-inner { padding: .75rem 1rem 1rem 1rem; border-top: 1px solid #2d3748; }

/* HTTP call cards */
.call-card { background: #0f1117; border-radius: 6px; margin-bottom: .75rem; overflow: hidden; }
.call-card:last-child { margin-bottom: 0; }
.call-url { display: flex; align-items: center; gap: .5rem; padding: .5rem .75rem; background: #161c29; border-bottom: 1px solid #1e2740; }
.method { font-weight: 700; font-size: .78rem; padding: .15rem .4rem; border-radius: 3px; }
.method-GET { background: #16543033; color: #4ade80; }
.method-POST { background: #1d4ed833; color: #93c5fd; }
.method-PUT { background: #78350f33; color: #fbbf24; }
.method-PATCH { background: #713f1233; color: #fdba74; }
.method-DELETE { background: #7f1d1d33; color: #f87171; }
.url-text { font-size: .8rem; color: #94a3b8; font-family: monospace; }
.call-dur { margin-left: auto; font-size: .75rem; color: #475569; }
.call-body { padding: .5rem .75rem; }
.call-section-label { font-size: .7rem; text-transform: uppercase; letter-spacing: .05em; color: #475569; margin-bottom: .25rem; margin-top: .5rem; }
pre.code { background: #0a0d14; border: 1px solid #1e2740; border-radius: 4px; padding: .5rem .75rem; font-size: .75rem; font-family: "SF Mono", Menlo, Consolas, monospace; color: #cbd5e1; overflow-x: auto; white-space: pre-wrap; word-break: break-all; max-height: 300px; overflow-y: auto; }
pre.routing-headers { background: #0c1220; border: 1px solid #1e3a5f; border-radius: 4px; padding: .5rem .75rem; font-size: .75rem; font-family: "SF Mono", Menlo, Consolas, monospace; color: #93c5fd; overflow-x: auto; white-space: pre-wrap; }
.stack-trace { font-family: "SF Mono", Menlo, Consolas, monospace; font-size: .72rem; color: #64748b; background: #0a0d14; border: 1px solid #1e2740; border-radius: 4px; padding: .4rem .6rem; }
.stack-trace .frame { padding: .1rem 0; }
.stack-trace .frame .loc { color: #7c3aed; }
.stack-trace .frame .fn { color: #94a3b8; }
.stack-trace .frame .arrow { color: #334155; margin-right: .3rem; }
.db-timeline { display: flex; align-items: center; gap: .5rem; flex-wrap: wrap; margin: .4rem 0; }
.db-op { display: flex; align-items: center; gap: .3rem; padding: .25rem .55rem; border-radius: 5px; font-size: .75rem; font-weight: 600; }
.db-op-reader { background: #065f4633; border: 1px solid #065f4655; color: #6ee7b7; }
.db-op-write { background: #7c3aed33; border: 1px solid #7c3aed55; color: #c4b5fd; }
.db-op-pinned { background: #1d4ed833; border: 1px solid #1d4ed855; color: #93c5fd; }
.db-op-tx { background: #78350f33; border: 1px solid #78350f55; color: #fbbf24; }
.db-op-forced { background: #831843aa; border: 1px solid #9f1239; color: #fda4af; }
.db-arrow { color: #334155; font-size: .9rem; }
.db-pin-badge { font-size: .7rem; padding: .1rem .35rem; border-radius: 3px; background: #7c3aed22; color: #c4b5fd; border: 1px solid #7c3aed55; margin-left: .25rem; }
.resp-status { display: inline-block; padding: .15rem .5rem; border-radius: 4px; font-size: .78rem; font-weight: 700; margin-bottom: .25rem; }
.status-2xx { background: #16543033; color: #4ade80; border: 1px solid #16543055; }
.status-4xx { background: #78350f33; color: #fbbf24; border: 1px solid #78350f55; }
.status-5xx { background: #7f1d1d33; color: #f87171; border: 1px solid #7f1d1d55; }
.controls { display: flex; gap: .5rem; margin-bottom: 1rem; }
button { background: #1e2330; border: 1px solid #334155; color: #94a3b8; padding: .35rem .75rem; border-radius: 5px; cursor: pointer; font-size: .8rem; }
button:hover { background: #252d3f; color: #e2e8f0; }
.pin-indicator { font-size: .7rem; padding: .1rem .35rem; border-radius: 3px; }
.pin-yes { background: #7c3aed22; color: #c4b5fd; }
.pin-no { color: #334155; }
</style>
</head>
<body>
<div class="container">
`, time.Now().Format("2006-01-02 15:04:05"))

	// ── Summary card ────────────────────────────────────────────────────────
	fmt.Fprintf(w, `
<div class="summary-card">
  <div class="title">
    <h1>FlexPrice Integration Test Report</h1>
    <small>Generated %s · Duration %.1fs · %d API calls captured</small>
  </div>
  <div class="stat"><div class="num pass-num">%d</div><div class="label">Passed</div></div>
  <div class="stat"><div class="num fail-num">%d</div><div class="label">Failed</div></div>
  <div class="stat"><div class="num skip-num">%d</div><div class="label">Skipped</div></div>
  <div class="stat"><div class="num" style="color:#94a3b8">%d</div><div class="label">Total</div></div>
</div>
`, time.Now().Format("2006-01-02 15:04:05"), totalDuration.Seconds(), len(calls),
		passed, failed, skipped, len(results))

	// ── Writer pinning analysis ──────────────────────────────────────────────
	if len(pinTimeline) > 0 {
		fmt.Fprintf(w, `
<div class="routing-section">
<h2>Writer Pinning &amp; DB Routing Analysis</h2>
<table>
<thead>
<tr>
  <th>Step</th>
  <th>Name</th>
  <th>API Calls</th>
  <th>Write?</th>
  <th>Pin Sent?</th>
  <th>Reader Calls</th>
  <th>Writer Pinned</th>
  <th>Writer Calls</th>
</tr>
</thead>
<tbody>
`)
		for _, pe := range pinTimeline {
			stepBadge := `<span class="badge badge-pass">PASS</span>`
			if !pe.StepPassed {
				stepBadge = `<span class="badge badge-fail">FAIL</span>`
			}
			writeCell := `<span class="num-gray">—</span>`
			if pe.WriteOccurred {
				writeCell = `<span class="badge badge-write">✓ WRITE</span>`
			}
			pinSentCell := `<span class="num-gray">—</span>`
			if pe.PinSent {
				pinSentCell = `<span class="badge badge-pin-sent">✓ PINNED</span>`
			}
			readerCell := `<span class="num-gray">0</span>`
			if pe.Reader > 0 {
				readerCell = fmt.Sprintf(`<span class="num-green">%d</span>`, pe.Reader)
			}
			wpCell := `<span class="num-gray">0</span>`
			if pe.WriterPinned > 0 {
				wpCell = fmt.Sprintf(`<span class="num-blue">%d</span>`, pe.WriterPinned)
			}
			wcCell := `<span class="num-gray">0</span>`
			if pe.WriterCalls > 0 {
				wcCell = fmt.Sprintf(`<span class="num-yellow">%d</span>`, pe.WriterCalls)
			}

			// Build URL list
			urlList := ""
			for _, u := range pe.URLs {
				urlList += `<code style="display:block;font-size:.72rem;color:#64748b;font-family:monospace">` + html.EscapeString(u) + `</code>`
			}

			fmt.Fprintf(w, `<tr>
  <td>%s <span style="color:#475569;font-size:.8rem">#%d</span></td>
  <td style="font-size:.82rem">%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
  <td>%s</td>
</tr>
`, stepBadge, pe.StepNumber, html.EscapeString(pe.StepName), urlList,
				writeCell, pinSentCell, readerCell, wpCell, wcCell)
		}
		fmt.Fprintf(w, `</tbody></table></div>`)
	}

	// ── Cross-request pin flow timeline ─────────────────────────────────────
	type pinFlowEntry struct {
		StepNumber int
		StepName   string
		Passed     bool
		EmittedPin bool
		UsedPin    bool
	}
	var pinFlow []pinFlowEntry
	for _, r := range results {
		if r.Skipped {
			continue
		}
		sc := callsByStep[r.StepNumber]
		var emitted, used bool
		for _, c := range sc {
			if c.PinUntilMs > 0 {
				emitted = true
			}
			if c.PinSent {
				used = true
			}
		}
		if emitted || used {
			pinFlow = append(pinFlow, pinFlowEntry{
				StepNumber: r.StepNumber,
				StepName:   r.StepName,
				Passed:     r.Passed,
				EmittedPin: emitted,
				UsedPin:    used,
			})
		}
	}

	if len(pinFlow) > 0 {
		fmt.Fprintf(w, `
<div class="routing-section" style="margin-bottom:1.5rem">
<h2>Cross-Request Writer Pin Flow</h2>
<p style="color:#64748b;font-size:.82rem;margin-bottom:1rem">
  Each write response emits <code style="color:#c4b5fd">X-Writer-Pinned-Until</code>.
  The test client sends <code style="color:#93c5fd">X-Pin-To-Writer: true</code> on
  subsequent requests within the pin window so the server routes those reads to the writer.
</p>
<div style="display:flex;align-items:flex-start;gap:.4rem;flex-wrap:wrap;padding:.5rem 0">
`)
		for i, pf := range pinFlow {
			stepColor := "#4ade80"
			if !pf.Passed {
				stepColor = "#f87171"
			}
			style := fmt.Sprintf(`border:1px solid %s33;background:%s11;`, stepColor, stepColor)

			var tags string
			if pf.EmittedPin {
				tags += `<div style="font-size:.68rem;color:#c4b5fd;margin-top:.2rem">&#x270f;&#xfe0f; emits pin</div>`
			}
			if pf.UsedPin {
				tags += `<div style="font-size:.68rem;color:#93c5fd;margin-top:.2rem">&#x1f4cc; uses pin</div>`
			}

			fmt.Fprintf(w, `<div style="border-radius:6px;padding:.4rem .6rem;%s min-width:120px;text-align:center">
  <div style="font-size:.68rem;color:#475569">#%d</div>
  <div style="font-size:.75rem;color:%s;font-weight:600">%s</div>
  %s
</div>`, style, pf.StepNumber, stepColor, html.EscapeString(truncate(pf.StepName, 28)), tags)

			if i < len(pinFlow)-1 {
				fmt.Fprintf(w, `<div style="align-self:center;color:#334155;font-size:1.2rem">&#x2192;</div>`)
			}
		}
		fmt.Fprintf(w, `</div></div>`)
	}

	// ── Per-step detail ──────────────────────────────────────────────────────
	fmt.Fprintf(w, `
<div class="steps-section">
<div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:.75rem">
  <h2 style="margin-bottom:0">Step-by-Step Detail</h2>
  <div class="controls">
    <button onclick="document.querySelectorAll('details').forEach(d=>d.open=true)">Expand All</button>
    <button onclick="document.querySelectorAll('details').forEach(d=>d.open=false)">Collapse All</button>
    <button onclick="document.querySelectorAll('details').forEach(d=>{if(!d.dataset.pass)d.open=true})">Expand Failures</button>
  </div>
</div>
`)

	for _, r := range results {
		statusClass := "status-pass"
		statusLabel := "PASS"
		if r.Skipped {
			statusClass = "status-skip"
			statusLabel = "SKIP"
		} else if !r.Passed {
			statusClass = "status-fail"
			statusLabel = "FAIL"
		}

		// Extra badges for routing on this step
		var stepBadges string
		if r.Routing.WriterCalls > 0 {
			stepBadges += fmt.Sprintf(` <span class="badge badge-write">%d writes</span>`, r.Routing.WriterCalls)
		}
		if r.Routing.WriterPinned > 0 {
			stepBadges += fmt.Sprintf(` <span class="badge badge-pin-sent">%d pinned reads</span>`, r.Routing.WriterPinned)
		}
		if r.Routing.Reader > 0 {
			stepBadges += fmt.Sprintf(` <span class="badge badge-read">%d replica reads</span>`, r.Routing.Reader)
		}

		passProp := ""
		if r.Passed {
			passProp = ` data-pass="1"`
		}

		fmt.Fprintf(w, `<div class="step-item">
<details%s>
<summary>
<div class="step-header">
  <span class="step-num">%d</span>
  <span class="step-status %s">%s</span>
  <span class="step-name">%s</span>
  <span class="step-meta">%s<span class="step-dur">%dms</span></span>
</div>
</summary>
`, passProp, r.StepNumber, statusClass, statusLabel, html.EscapeString(r.StepName), stepBadges, r.Duration.Milliseconds())

		// Error / details
		if !r.Passed && !r.Skipped && r.Error != nil {
			fmt.Fprintf(w, `<div class="step-err">Error: %s</div>`, html.EscapeString(r.Error.Error()))
		}
		if r.Details != "" {
			fmt.Fprintf(w, `<div class="step-err" style="color:#94a3b8">%s</div>`, html.EscapeString(r.Details))
		}

		// HTTP calls for this step
		stepCalls := callsByStep[r.StepNumber]
		if len(stepCalls) > 0 {
			fmt.Fprintf(w, `<div class="step-details-inner">`)
			for _, c := range stepCalls {
				renderCallCard(w, c)
			}
			fmt.Fprintf(w, `</div>`)
		} else if r.Skipped {
			fmt.Fprintf(w, `<div class="step-details-inner" style="color:#475569;font-size:.82rem">Skipped — no HTTP calls made.</div>`)
		}

		fmt.Fprintf(w, `</details></div>`)
	}

	fmt.Fprintf(w, `</div>`) // steps-section

	// ── Footer ───────────────────────────────────────────────────────────────
	fmt.Fprintf(w, `
<div style="color:#334155;font-size:.75rem;text-align:center;padding:1rem 0">
  FlexPrice reader-writer pinning integration report · %s
</div>
</div></body></html>
`, time.Now().Format(time.RFC3339))

	return os.WriteFile(reportPath, buf.Bytes(), 0644)
}

// renderCallCard writes one HTTP call card into w.
func renderCallCard(w *bytes.Buffer, c CallRecord) {
	methodClass := "method-" + c.Method
	if c.Method == "" {
		methodClass = ""
	}

	// Shorten URL for display.
	displayURL := c.URL
	if idx := strings.Index(displayURL, "/v1/"); idx >= 0 {
		displayURL = displayURL[idx:]
	}

	// Pin indicators on the call URL line.
	pinHtml := ""
	if c.PinSent {
		pinHtml += ` <span class="pin-indicator pin-yes">X-Pin-To-Writer: true</span>`
	}
	if c.PinUntilMs > 0 {
		pinHtml += ` <span class="pin-indicator pin-yes">write → pin until ` + fmt.Sprintf("%d", c.PinUntilMs) + `</span>`
	}

	fmt.Fprintf(w, `<div class="call-card">
<div class="call-url">
  <span class="method %s">%s</span>
  <span class="url-text">%s</span>%s
  <span class="call-dur">%dms</span>
</div>
<div class="call-body">
`, methodClass, html.EscapeString(c.Method), html.EscapeString(displayURL), pinHtml, c.Duration.Milliseconds())

	// ── Call site (stack trace) ──────────────────────────────────────────
	if len(c.TestStackTrace) > 0 {
		fmt.Fprintf(w, `<div class="call-section-label">Called from</div><div class="stack-trace">`)
		for _, frame := range c.TestStackTrace {
			fmt.Fprintf(w, `<div class="frame"><span class="arrow">↳</span><span class="loc">%s:%d</span>  <span class="fn">%s</span></div>`,
				html.EscapeString(frame.File), frame.Line, html.EscapeString(frame.Function))
		}
		fmt.Fprintf(w, `</div>`)
	}

	// ── DB operation timeline ────────────────────────────────────────────
	r := c.Routing
	hasRouting := r.Reader+r.WriterPinned+r.WriterTx+r.WriterForced+r.WriterCalls > 0
	if hasRouting {
		fmt.Fprintf(w, `<div class="call-section-label">DB Operation Flow</div><div class="db-timeline">`)
		renderDBTimeline(w, r, c.PinSent, c.PinUntilMs > 0)
		fmt.Fprintf(w, `</div>`)
	}

	// Request headers (masked).
	masked := maskedHeaders(c.ReqHeaders)
	routingReqHeaders := extractRoutingHeaders(masked)
	otherReqHeaders := excludeRoutingHeaders(masked)

	if routingReqHeaders != "" {
		fmt.Fprintf(w, `<div class="call-section-label">Request · Routing Headers</div>`)
		fmt.Fprintf(w, `<pre class="routing-headers">%s</pre>`, html.EscapeString(routingReqHeaders))
	}
	if otherReqHeaders != "" {
		fmt.Fprintf(w, `<div class="call-section-label">Request · Headers</div>`)
		fmt.Fprintf(w, `<pre class="code">%s</pre>`, html.EscapeString(otherReqHeaders))
	}
	if len(c.ReqBody) > 0 {
		fmt.Fprintf(w, `<div class="call-section-label">Request · Body</div>`)
		fmt.Fprintf(w, `<pre class="code">%s</pre>`, html.EscapeString(prettyJSON(c.ReqBody)))
	}

	// Response.
	statusClass := "status-2xx"
	if c.RespStatus >= 500 {
		statusClass = "status-5xx"
	} else if c.RespStatus >= 400 {
		statusClass = "status-4xx"
	}

	fmt.Fprintf(w, `<div class="call-section-label">Response</div>`)
	fmt.Fprintf(w, `<span class="resp-status %s">%d</span>`, statusClass, c.RespStatus)

	// Response routing headers (highlighted).
	routingRespHeaders := extractRoutingHeaders(c.RespHeaders)
	otherRespHeaders := excludeRoutingHeaders(c.RespHeaders)

	if routingRespHeaders != "" {
		fmt.Fprintf(w, `<div class="call-section-label">Response · Routing Headers</div>`)
		fmt.Fprintf(w, `<pre class="routing-headers">%s</pre>`, html.EscapeString(routingRespHeaders))
	}
	if otherRespHeaders != "" {
		fmt.Fprintf(w, `<div class="call-section-label">Response · Headers</div>`)
		fmt.Fprintf(w, `<pre class="code">%s</pre>`, html.EscapeString(otherRespHeaders))
	}
	if len(c.RespBody) > 0 {
		body := prettyJSON(c.RespBody)
		if len(body) > 3000 {
			body = body[:3000] + "\n… [truncated]"
		}
		fmt.Fprintf(w, `<div class="call-section-label">Response · Body</div>`)
		fmt.Fprintf(w, `<pre class="code">%s</pre>`, html.EscapeString(body))
	}

	fmt.Fprintf(w, `</div></div>`) // call-body, call-card
}

// renderDBTimeline writes a horizontal flow of DB operation boxes for one
// HTTP call. It infers the sequence from the routing counts:
//
//	[pre-write reads] → [writes] → [pinned reads / tx reads] → [forced reads]
//
// This matches the typical Postgres client flow: validate/fetch first (Reader),
// write next (Writer call which pins the context), then post-write reads go to
// the writer via the pin (WriterPinned) or transaction (WriterTx).
func renderDBTimeline(w *bytes.Buffer, r RoutingHeaders, pinSent bool, writeEmittedPin bool) {
	first := true
	arrow := func() {
		if !first {
			fmt.Fprintf(w, `<span class="db-arrow">→</span>`)
		}
		first = false
	}

	// Step 0: cross-request pin was active on this request (client sent X-Pin-To-Writer).
	if pinSent {
		arrow()
		fmt.Fprintf(w, `<span class="db-op db-op-pinned">🔒 cross-request pin active</span>`)
	}

	// Step 1: replica reads (happen before writes in typical validation paths).
	if r.Reader > 0 {
		arrow()
		label := "read"
		if r.Reader > 1 {
			label = fmt.Sprintf("%d reads", r.Reader)
		}
		fmt.Fprintf(w, `<span class="db-op db-op-reader">📖 %s → replica</span>`, label)
	}

	// Step 2: write calls (INSERT / UPDATE / DELETE → pins the context).
	if r.WriterCalls > 0 {
		arrow()
		label := "write"
		if r.WriterCalls > 1 {
			label = fmt.Sprintf("%d writes", r.WriterCalls)
		}
		pin := ""
		if writeEmittedPin {
			pin = ` <span class="db-pin-badge">→ pins client</span>`
		}
		fmt.Fprintf(w, `<span class="db-op db-op-write">✏️ %s → writer%s</span>`, label, pin)
	}

	// Step 3: reads inside a DB transaction (always go to writer).
	if r.WriterTx > 0 {
		arrow()
		label := "read"
		if r.WriterTx > 1 {
			label = fmt.Sprintf("%d reads", r.WriterTx)
		}
		fmt.Fprintf(w, `<span class="db-op db-op-tx">🔄 %s → writer (tx)</span>`, label)
	}

	// Step 4: reads pinned to writer because a prior write set the context pin.
	if r.WriterPinned > 0 {
		arrow()
		label := "read"
		if r.WriterPinned > 1 {
			label = fmt.Sprintf("%d reads", r.WriterPinned)
		}
		fmt.Fprintf(w, `<span class="db-op db-op-pinned">📌 %s → writer (pinned)</span>`, label)
	}

	// Step 5: force-writer reads (explicit WithForceWriter).
	if r.WriterForced > 0 {
		arrow()
		label := "read"
		if r.WriterForced > 1 {
			label = fmt.Sprintf("%d reads", r.WriterForced)
		}
		fmt.Fprintf(w, `<span class="db-op db-op-forced">⚡ %s → writer (forced)</span>`, label)
	}

	// Edge case: no individual categories but WriterCalls was > 0 already handled.
}

// extractRoutingHeaders returns only the routing-related header lines.
func extractRoutingHeaders(h http.Header) string {
	var sb strings.Builder
	for _, key := range routingHeaderKeys {
		if vs := h[http.CanonicalHeaderKey(key)]; len(vs) > 0 {
			sb.WriteString(key + ": " + strings.Join(vs, ", ") + "\n")
		}
	}
	return sb.String()
}

// excludeRoutingHeaders returns header lines that are NOT routing-related.
func excludeRoutingHeaders(h http.Header) string {
	var sb strings.Builder
	for k, vs := range h {
		isRouting := false
		for _, rk := range routingHeaderKeys {
			if strings.EqualFold(k, rk) {
				isRouting = true
				break
			}
		}
		if isRouting {
			continue
		}
		sb.WriteString(k + ": " + strings.Join(vs, ", ") + "\n")
	}
	return sb.String()
}

// prettyJSON attempts to pretty-print JSON bytes; returns original string on failure.
func prettyJSON(b []byte) string {
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return string(b)
	}
	return string(out)
}
