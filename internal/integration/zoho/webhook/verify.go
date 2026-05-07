package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strings"
)

// BuildSigningString builds the string Zoho signs for webhook verification per Zoho Billing / Books guidance:
// sort query key-value pairs alphabetically (concatenate key+value for each pair), then append the raw JSON body bytes.
// See: https://www.zoho.com/billing/kb/webhooks/securing-webhooks.html
func BuildSigningString(parsedURL *url.URL, rawBody []byte) string {
	type pair struct{ k, v string }
	var pairs []pair
	if parsedURL != nil {
		q := parsedURL.Query()
		for k, vals := range q {
			for _, v := range vals {
				pairs = append(pairs, pair{k: k, v: v})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].k != pairs[j].k {
			return pairs[i].k < pairs[j].k
		}
		return pairs[i].v < pairs[j].v
	})
	var b strings.Builder
	for _, p := range pairs {
		b.WriteString(p.k)
		b.WriteString(p.v)
	}
	b.Write(rawBody)
	return b.String()
}

// VerifySignature compares X-Zoho-Webhook-Signature with HMAC-SHA256(signingString, secret) as lowercase hex.
func VerifySignature(signature, signingString, secret string) bool {
	if strings.TrimSpace(signature) == "" || secret == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(signingString))
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(strings.ToLower(strings.TrimSpace(expected))), []byte(strings.ToLower(strings.TrimSpace(signature))))
}
