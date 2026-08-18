// Package spec loads the embedded OpenAPI document and derives the CLI's
// command surface from it.
package spec

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/getkin/kin-openapi/openapi3"

	specdata "github.com/flexprice/cli/spec"
)

// WebhookEventsTag marks 56 documentation stubs that describe webhook payload
// schemas. They have no operationId, their paths are synthetic, and calling them
// 404s — so they are excluded from commands but kept as the authoritative list of
// event types. Design doc §5.
const WebhookEventsTag = "Webhook Events"

// Memoizes Load: parsing the ~880KB spec costs tens of milliseconds and a
// single invocation calls it 2-3 times. The returned *openapi3.T is shared, so
// callers must treat it as read-only.
var (
	loadOnce sync.Once
	loadDoc  *openapi3.T
	loadErr  error
)

func Load() (*openapi3.T, error) {
	loadOnce.Do(func() {
		loader := openapi3.NewLoader()
		doc, err := loader.LoadFromData(specdata.OpenAPI)
		if err != nil {
			loadErr = fmt.Errorf("parse embedded OpenAPI spec: %w", err)
			return
		}
		loadDoc = doc
	})
	return loadDoc, loadErr
}

type Region struct {
	Key         string
	BaseURL     string
	Description string
}

// Regions derives the region list from servers[], so adding a region to the spec
// makes the next build offer it with no code change. Design doc §6.
func Regions(doc *openapi3.T) []Region {
	var out []Region
	for _, s := range doc.Servers {
		out = append(out, Region{
			Key:         regionKey(s.URL, s.Description),
			BaseURL:     s.URL,
			Description: s.Description,
		})
	}
	return out
}

// regionKey produces a short flag-friendly key: "US Region" -> us, "India Region" -> in.
// Falls back to a slug derived from the URL host when the description is missing or
// blank, since strings.Fields on an empty description would otherwise panic on index 0.
func regionKey(url, description string) string {
	fields := strings.Fields(description)
	if len(fields) == 0 {
		return hostSlug(url)
	}
	word := strings.ToLower(fields[0])
	switch word {
	case "india":
		return "in"
	case "united", "usa":
		return "us"
	}
	if len(word) <= 3 {
		return word
	}
	return word[:2]
}

// hostSlug derives a short key from a server URL's host when no usable
// description is present, e.g. "https://us.api.flexprice.io/v1" -> "us".
func hostSlug(url string) string {
	host := strings.TrimPrefix(url, "https://")
	host = strings.TrimPrefix(host, "http://")
	if i := strings.IndexByte(host, '/'); i >= 0 {
		host = host[:i]
	}
	if i := strings.IndexByte(host, '.'); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		return "region"
	}
	return strings.ToLower(host)
}

// Operation is one callable API operation.
type Operation struct {
	ID     string
	Method string
	Path   string
	Tag    string
	Op     *openapi3.Operation
	Item   *openapi3.PathItem
}

// Operations returns every callable operation, excluding the webhook stubs and
// anything without an operationId.
func Operations(doc *openapi3.T) []Operation {
	var out []Operation
	for path, item := range doc.Paths.Map() {
		for method, op := range item.Operations() {
			if op.OperationID == "" {
				continue
			}
			tag := ""
			if len(op.Tags) > 0 {
				tag = op.Tags[0]
			}
			if tag == WebhookEventsTag {
				continue
			}
			out = append(out, Operation{
				ID: op.OperationID, Method: method, Path: path, Tag: tag, Op: op, Item: item,
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// EventTypes reads webhook event names off the excluded stubs. These drive
// validation and completion for `trigger` and `listen --events`.
func EventTypes(doc *openapi3.T) []string {
	var out []string
	for path, item := range doc.Paths.Map() {
		for _, op := range item.Operations() {
			isStub := false
			for _, t := range op.Tags {
				if t == WebhookEventsTag {
					isStub = true
				}
			}
			if !isStub {
				continue
			}
			if name := strings.TrimPrefix(path, "/webhook-events/"); name != path {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}
