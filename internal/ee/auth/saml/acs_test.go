package saml

import (
	"context"
	"testing"

	"github.com/crewjam/saml"

	"github.com/flexprice/flexprice/internal/domain/user"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

func assertionWithNameID(value string) *saml.Assertion {
	return &saml.Assertion{
		Subject: &saml.Subject{
			NameID: &saml.NameID{Value: value},
		},
	}
}

func assertionWithAttribute(name, value string) *saml.Assertion {
	return &saml.Assertion{
		AttributeStatements: []saml.AttributeStatement{
			{
				Attributes: []saml.Attribute{
					{
						Name:   name,
						Values: []saml.AttributeValue{{Value: value}},
					},
				},
			},
		},
	}
}

// TestExtractEmailFromNameID covers the default path, where the identity
// provider sends the email as the NameID.
func TestExtractEmailFromNameID(t *testing.T) {
	got, err := extractEmail(assertionWithNameID("Alice@Example.com"), Config{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Casing is normalised so an identity provider that changes case cannot
	// create a second account for someone who already exists.
	if got != "alice@example.com" {
		t.Errorf("got %q, want the lowercased address", got)
	}
}

func TestExtractEmailFromConfiguredAttribute(t *testing.T) {
	cfg := Config{EmailAttribute: "http://schemas.xmlsoap.org/ws/2005/05/identity/claims/emailaddress"}
	assertion := assertionWithAttribute(cfg.EmailAttribute, "bob@example.com")

	got, err := extractEmail(assertion, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "bob@example.com" {
		t.Errorf("got %q, want bob@example.com", got)
	}
}

// TestExtractEmailRejectsUnusableAssertions is the important one: an assertion
// that authenticates someone we cannot identify must fail, never fall back to a
// guess. A wrong answer here logs a user into the wrong account.
func TestExtractEmailRejectsUnusableAssertions(t *testing.T) {
	cases := []struct {
		name      string
		assertion *saml.Assertion
		cfg       Config
	}{
		{
			name:      "no subject at all",
			assertion: &saml.Assertion{},
		},
		{
			name:      "NameID is an opaque identifier, not an email",
			assertion: assertionWithNameID("a7f3c9e1-persistent-id"),
		},
		{
			name:      "NameID empty",
			assertion: assertionWithNameID("   "),
		},
		{
			name:      "configured attribute absent",
			assertion: assertionWithAttribute("some_other_claim", "carol@example.com"),
			cfg:       Config{EmailAttribute: "email"},
		},
		{
			name:      "configured attribute present but empty",
			assertion: assertionWithAttribute("email", "  "),
			cfg:       Config{EmailAttribute: "email"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractEmail(tc.assertion, tc.cfg)
			if err == nil {
				t.Fatalf("expected rejection, got email %q", got)
			}
		})
	}
}

// TestConfiguredAttributeDoesNotFallBackToNameID pins a deliberate choice: when
// an administrator names an email attribute, a NameID must not silently satisfy
// it. Falling back would mean a misconfigured attribute quietly authenticates
// users off whatever the identity provider happens to put in the NameID.
func TestConfiguredAttributeDoesNotFallBackToNameID(t *testing.T) {
	assertion := assertionWithNameID("dave@example.com")
	cfg := Config{EmailAttribute: "email"}

	if got, err := extractEmail(assertion, cfg); err == nil {
		t.Errorf("configured attribute fell back to NameID, yielding %q", got)
	}
}

// TestResolveUserLooksUpAcrossTenants pins the fix for a real failure found by
// live testing: the lookup ran with the assertion's tenant in context, and
// GetByEmail filters by tenant when one is set. A user belonging to another
// organisation was therefore invisible, so the flow fell through to
// provisioning and hit the global unique index on users.email — surfacing as a
// 500 database error instead of the explicit 403 refusal.
//
// The lookup must clear the tenant so the cross-tenant branch is reachable.
func TestResolveUserLooksUpAcrossTenants(t *testing.T) {
	const (
		ownerTenant = "tenant_owner"
		otherTenant = "tenant_other"
		email       = "person@example.com"
	)

	existing := user.NewUser(email, ownerTenant)
	repo := &tenantScopedUserRepo{users: []*user.User{existing}}
	params := service.ServiceParams{UserRepo: repo}
	provider := newTestProvider("https://billing.example.com")

	// The assertion's tenant is already in context by the time resolveUser runs,
	// exactly as the ACS handler leaves it. That is what makes the bug possible:
	// a lookup inheriting this tenant cannot see a user who belongs to another
	// one. A context without a tenant would pass whether or not resolveUser
	// clears it, and would test nothing.
	ctx := types.SetTenantID(context.Background(), otherTenant)

	// An assertion validated for a tenant that does not own this email must be
	// refused, not silently logged into the wrong tenant and not surfaced as a
	// database error from a duplicate insert.
	_, err := provider.resolveUser(ctx, params, otherTenant, email, enabledConfig())
	if err == nil {
		t.Fatal("a user belonging to another organisation must not be authenticated")
	}
	if !ierr.IsPermissionDenied(err) {
		t.Errorf("error = %v, want permission denied rather than a database failure", err)
	}
	if repo.created != 0 {
		t.Errorf("provisioning ran %d time(s); the cross-tenant branch must refuse before provisioning", repo.created)
	}

	// The owning tenant still resolves to the existing user rather than
	// provisioning a second one.
	id, err := provider.resolveUser(types.SetTenantID(context.Background(), ownerTenant), params, ownerTenant, email, enabledConfig())
	if err != nil {
		t.Fatalf("the owning tenant must authenticate its own user: %v", err)
	}
	if id != existing.ID {
		t.Errorf("resolved id = %q, want the existing user %q", id, existing.ID)
	}
	if repo.created != 0 {
		t.Errorf("provisioning ran for a user that already exists")
	}

	// An unknown email in a configured tenant is provisioned.
	if _, err := provider.resolveUser(ctx, params, otherTenant, "new@example.com", enabledConfig()); err != nil {
		t.Fatalf("an unknown email must be provisioned: %v", err)
	}
	if repo.created != 1 {
		t.Errorf("provisioning ran %d time(s), want exactly 1", repo.created)
	}
}

// tenantScopedUserRepo reproduces the behaviour that made the bug possible:
// GetByEmail filters by the tenant in context, so a lookup carrying a tenant
// cannot see a user who belongs to another one. The in-memory store in
// internal/testutil ignores tenant entirely and would pass whether or not
// resolveUser clears it.
type tenantScopedUserRepo struct {
	users   []*user.User
	created int
}

func (r *tenantScopedUserRepo) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	tenantID := types.GetTenantID(ctx)
	for _, u := range r.users {
		if u.Email != email {
			continue
		}
		if tenantID != "" && u.TenantID != tenantID {
			continue
		}
		return u, nil
	}
	return nil, ierr.NewError("user not found").Mark(ierr.ErrNotFound)
}

func (r *tenantScopedUserRepo) Create(ctx context.Context, u *user.User) error {
	r.created++
	r.users = append(r.users, u)
	return nil
}

func (r *tenantScopedUserRepo) GetByID(context.Context, string) (*user.User, error) {
	return nil, ierr.NewError("not implemented").Mark(ierr.ErrNotFound)
}
func (r *tenantScopedUserRepo) Update(context.Context, *user.User) error            { return nil }
func (r *tenantScopedUserRepo) UpdateRoles(context.Context, string, []string) error { return nil }
func (r *tenantScopedUserRepo) Delete(context.Context, string) error                { return nil }
func (r *tenantScopedUserRepo) ListByFilter(context.Context, *types.UserFilter) ([]*user.User, int64, error) {
	return nil, 0, nil
}

// TestExtractEmailRejectsNonEmailAttribute closes the gap between the two
// extraction paths. The NameID path has always required an "@"; the attribute
// path did not, so an identity provider mapping the configured attribute to a
// username or an employee number produced a user whose email column held that
// value. users.email is globally unique, so the row is permanent and blocks the
// real address from ever being provisioned.
func TestExtractEmailRejectsNonEmailAttribute(t *testing.T) {
	cfg := Config{EmailAttribute: "email"}

	for _, value := range []string{"jsmith", "EMP-00417", "user1"} {
		if _, err := extractEmail(assertionWithAttribute("email", value), cfg); err == nil {
			t.Errorf("attribute value %q was accepted as an email address", value)
		}
	}

	// A real address still works, and is still normalised.
	got, err := extractEmail(assertionWithAttribute("email", "  Alice@Example.com "), cfg)
	if err != nil {
		t.Fatalf("a valid address must be accepted: %v", err)
	}
	if got != "alice@example.com" {
		t.Errorf("extractEmail() = %q, want it trimmed and lowercased", got)
	}
}
