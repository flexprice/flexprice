package saml

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"

	"github.com/crewjam/saml"
)

// parseCertificate decodes the identity provider's PEM-encoded X.509 signing
// certificate. Assertions are verified against this and only this — a
// certificate carried inside a response is attacker-controlled and never used.
func parseCertificate(pemData string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	if block.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("expected a CERTIFICATE block, got %q", block.Type)
	}
	return x509.ParseCertificate(block.Bytes)
}

// serviceProvider builds the SP for one tenant.
//
// crewjam/saml performs signature verification, XML signature wrapping
// detection, audience restriction, and condition-window checks against the
// IDPMetadata we supply here. Supplying the certificate through IDPMetadata,
// rather than trusting the response, is what makes those checks meaningful.
func (p *samlProvider) serviceProvider(tenantID string, cfg Config) (*saml.ServiceProvider, error) {
	cert, err := parseCertificate(cfg.IDPCertificate)
	if err != nil {
		return nil, fmt.Errorf("parse idp certificate: %w", err)
	}

	acsURL, err := url.Parse(p.acsURL(tenantID))
	if err != nil {
		return nil, fmt.Errorf("parse acs url: %w", err)
	}
	metadataURL, err := url.Parse(p.metadataURL(tenantID))
	if err != nil {
		return nil, fmt.Errorf("parse metadata url: %w", err)
	}
	ssoURL, err := url.Parse(cfg.IDPSSOUrl)
	if err != nil {
		return nil, fmt.Errorf("parse idp sso url: %w", err)
	}

	return &saml.ServiceProvider{
		EntityID:    metadataURL.String(),
		AcsURL:      *acsURL,
		MetadataURL: *metadataURL,
		IDPMetadata: &saml.EntityDescriptor{
			EntityID: cfg.IDPEntityID,
			IDPSSODescriptors: []saml.IDPSSODescriptor{
				{
					SSODescriptor: saml.SSODescriptor{
						RoleDescriptor: saml.RoleDescriptor{
							KeyDescriptors: []saml.KeyDescriptor{
								{
									Use: "signing",
									KeyInfo: saml.KeyInfo{
										X509Data: saml.X509Data{
											X509Certificates: []saml.X509Certificate{
												{Data: base64DER(cert)},
											},
										},
									},
								},
							},
						},
					},
					SingleSignOnServices: []saml.Endpoint{
						{
							Binding:  saml.HTTPRedirectBinding,
							Location: ssoURL.String(),
						},
					},
				},
			},
		},
	}, nil
}

// URLs are derived from one configured base so the values in our SP metadata
// always match the endpoints we actually serve. A mismatch here surfaces as an
// audience or destination rejection at the identity provider, which is painful
// to diagnose.
func (p *samlProvider) metadataURL(tenantID string) string {
	return fmt.Sprintf("%s/v1/auth/saml/%s/metadata", p.baseURL(), tenantID)
}

func (p *samlProvider) acsURL(tenantID string) string {
	return fmt.Sprintf("%s/v1/auth/saml/%s/acs", p.baseURL(), tenantID)
}

// metadataOnlyServiceProvider builds an SP carrying just our own identifiers,
// with no identity provider attached.
//
// It exists for the metadata endpoint, which has to work before a tenant has
// configured anything: the customer's administrator needs our ACS URL and
// entity ID in order to create the application that produces the values we
// would otherwise be validating.
//
// It must never be used to consume an assertion — with no IDPMetadata there is
// no certificate to verify a signature against.
func (p *samlProvider) metadataOnlyServiceProvider(tenantID string) (*saml.ServiceProvider, error) {
	acsURL, err := url.Parse(p.acsURL(tenantID))
	if err != nil {
		return nil, fmt.Errorf("parse acs url: %w", err)
	}
	metadataURL, err := url.Parse(p.metadataURL(tenantID))
	if err != nil {
		return nil, fmt.Errorf("parse metadata url: %w", err)
	}

	return &saml.ServiceProvider{
		EntityID:    metadataURL.String(),
		AcsURL:      *acsURL,
		MetadataURL: *metadataURL,
	}, nil
}
