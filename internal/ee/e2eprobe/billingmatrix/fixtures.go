package billingmatrix

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
)

// fixtureKeyPrefix marks shared, reused resources; the orphan sweep never touches them.
const fixtureKeyPrefix = "e2eprobe_bm_fx_"

// fixtures are resources shared by every scenario and reused across runs by stable lookup key:
// the usage meter, the entitlement-grant feature, and one addon per configuration. Addons are
// shared because the API refuses to delete an addon that was ever on a subscription.
type fixtures struct {
	c client

	mu       sync.Mutex
	features map[string]featureFixture
	addons   map[string]addonFixture
}

type featureFixture struct {
	featureID string
	meterID   string
}

type addonFixture struct {
	addonID       string
	entitlementID string
}

func newFixtures(api API, _ string) *fixtures {
	return &fixtures{c: client{api: api}, features: map[string]featureFixture{}, addons: map[string]addonFixture{}}
}

// usageMeter is the metered SUM feature usage prices bill against.
func (f *fixtures) usageMeter(ctx context.Context) (featureID, meterID string, err error) {
	ff, err := f.feature(ctx, fixtureKeyPrefix+"usage")
	return ff.featureID, ff.meterID, err
}

// feature finds or creates the metered feature with lookup key key.
func (f *fixtures) feature(ctx context.Context, key string) (featureFixture, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if ff, ok := f.features[key]; ok {
		return ff, nil
	}
	var page json.RawMessage
	if err := f.c.post(ctx, "/features/search", map[string]any{"lookup_key": key, "limit": 1}, &page); err != nil {
		return featureFixture{}, fmt.Errorf("find feature %s: %w", key, err)
	}
	var found []struct {
		ID        string `json:"id"`
		LookupKey string `json:"lookup_key"`
		MeterID   string `json:"meter_id"`
	}
	if err := decodeItems(page, &found); err != nil {
		return featureFixture{}, err
	}
	var ff featureFixture
	if len(found) > 0 && found[0].LookupKey == key {
		ff = featureFixture{featureID: found[0].ID, meterID: found[0].MeterID}
	} else {
		id, meterID, err := f.c.createMeteredFeature(ctx, key)
		if err != nil {
			return featureFixture{}, fmt.Errorf("create feature %s: %w", key, err)
		}
		ff = featureFixture{featureID: id, meterID: meterID}
	}
	f.features[key] = ff
	return ff, nil
}

// addonDef is an addon configuration: one fixed price, plus an optional credit grant or entitlement grant.
type addonDef struct {
	item        itemSpec
	creditGrant map[string]any // POST /creditgrants body without scope/addon_id
	grantQuota  string         // non-empty: additive subscription-period entitlement grant of this quota
}

func (d addonDef) key() string {
	price := d.item.priceBody("ADDON", "", "")
	delete(price, "start_date")
	raw, _ := json.Marshal(map[string]any{"price": price, "cg": d.creditGrant, "eg": d.grantQuota})
	sum := sha256.Sum256(raw)
	return fixtureKeyPrefix + "addon_" + hex.EncodeToString(sum[:])[:12]
}

// addon finds or creates the addon for def, adding any missing price or grant.
func (f *fixtures) addon(ctx context.Context, def addonDef) (addonFixture, error) {
	var egFeature featureFixture
	if def.grantQuota != "" {
		var err error
		if egFeature, err = f.feature(ctx, fixtureKeyPrefix+"eg"); err != nil {
			return addonFixture{}, err
		}
	}
	key := def.key()
	f.mu.Lock()
	defer f.mu.Unlock()
	if af, ok := f.addons[key]; ok {
		return af, nil
	}

	var existing struct {
		ID     string   `json:"id"`
		Prices []idOnly `json:"prices"`
	}
	err := f.c.get(ctx, "/addons/lookup/"+url.PathEscape(key), &existing)
	if err != nil && statusOf(err) != http.StatusNotFound {
		return addonFixture{}, fmt.Errorf("find addon %s: %w", key, err)
	}
	af := addonFixture{addonID: existing.ID}
	if af.addonID == "" {
		if af.addonID, err = f.c.createAddon(ctx, "e2eprobe-bm fixture "+def.item.key, key); err != nil {
			return addonFixture{}, fmt.Errorf("create addon %s: %w", key, err)
		}
	} else if err := f.c.get(ctx, "/addons/"+url.PathEscape(af.addonID), &existing); err != nil {
		return addonFixture{}, fmt.Errorf("get addon %s: %w", key, err)
	}
	if len(existing.Prices) == 0 {
		if _, err := f.c.createPrice(ctx, def.item.priceBody("ADDON", af.addonID, "")); err != nil {
			return addonFixture{}, fmt.Errorf("create addon price %s: %w", key, err)
		}
	}
	if def.creditGrant != nil {
		if err := f.ensureCreditGrant(ctx, af.addonID, def.creditGrant); err != nil {
			return addonFixture{}, err
		}
	}
	if def.grantQuota != "" {
		if af.entitlementID, err = f.ensureEntitlementGrant(ctx, af.addonID, egFeature.featureID, def.grantQuota); err != nil {
			return addonFixture{}, err
		}
	}
	f.addons[key] = af
	return af, nil
}

func (f *fixtures) ensureCreditGrant(ctx context.Context, addonID string, grant map[string]any) error {
	var raw json.RawMessage
	if err := f.c.get(ctx, "/addons/"+url.PathEscape(addonID)+"/creditgrants", &raw); err != nil {
		return fmt.Errorf("list addon credit grants: %w", err)
	}
	var grants []idOnly
	if err := decodeItems(raw, &grants); err != nil {
		return err
	}
	if len(grants) > 0 {
		return nil
	}
	body := map[string]any{"scope": "ADDON", "addon_id": addonID}
	for k, v := range grant {
		body[k] = v
	}
	if err := f.c.post(ctx, "/creditgrants", body, nil); err != nil {
		return fmt.Errorf("create addon credit grant: %w", err)
	}
	return nil
}

func (f *fixtures) ensureEntitlementGrant(ctx context.Context, addonID, featureID, quota string) (string, error) {
	var raw json.RawMessage
	if err := f.c.get(ctx, "/addons/"+url.PathEscape(addonID)+"/entitlements", &raw); err != nil {
		return "", fmt.Errorf("list addon entitlements: %w", err)
	}
	var ents []idOnly
	if err := decodeItems(raw, &ents); err != nil {
		return "", err
	}
	if len(ents) > 0 {
		return ents[0].ID, nil
	}
	var ent idOnly
	if err := f.c.post(ctx, "/entitlements", map[string]any{
		"feature_id": featureID, "feature_type": "metered", "is_enabled": true,
		"entity_type": "ADDON", "entity_id": addonID,
		"grant_measure": "quantity", "grant_quota": quota,
		"grant_duration_unit": "subscription_period", "aggregation_mode": "additive",
	}, &ent); err != nil {
		return "", fmt.Errorf("create addon entitlement: %w", err)
	}
	return ent.ID, nil
}
