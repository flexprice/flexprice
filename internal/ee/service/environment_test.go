package service

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"

	"github.com/flexprice/flexprice/internal/api/dto"
	"github.com/flexprice/flexprice/internal/config"
	"github.com/flexprice/flexprice/internal/domain/billingentity"
	"github.com/flexprice/flexprice/internal/domain/environment"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/logger"
	"github.com/flexprice/flexprice/internal/testutil"
	"github.com/flexprice/flexprice/internal/types"
	"github.com/samber/lo"
	"github.com/stretchr/testify/suite"
)

type EnvironmentServiceSuite struct {
	suite.Suite
	ctx                context.Context
	environmentService *environmentService
	environmentRepo    *testutil.InMemoryEnvironmentStore
}

func TestEnvironmentService(t *testing.T) {
	suite.Run(t, new(EnvironmentServiceSuite))
}

func (s *EnvironmentServiceSuite) SetupTest() {
	s.ctx = context.Background()
	s.ctx = context.WithValue(s.ctx, types.CtxTenantID, "test-tenant-id")
	s.environmentRepo = testutil.NewInMemoryEnvironmentStore()

	// Create env access service that allows all access
	cfg := &config.Configuration{
		EnvAccess: config.EnvAccessConfig{
			UserEnvMapping: nil, // nil means all users are super admin
		},
	}
	envAccessService := NewEnvAccessService(cfg)

	// Create a real settings service for test (needed for generic GetSetting function)
	settingsRepo := testutil.NewInMemorySettingsStore()
	serviceParams := ServiceParams{
		SettingsRepo: settingsRepo,
		DB:           testutil.NewMockPostgresClient(logger.NewNoopLogger()),
	}
	realSettingsService := NewSettingsService(serviceParams)

	s.environmentService = &environmentService{
		repo:             s.environmentRepo,
		envAccessService: envAccessService,
		settingsService:  realSettingsService,
		ServiceParams:    serviceParams,
	}
}

func (s *EnvironmentServiceSuite) TestCreateEnvironment() {
	req := dto.CreateEnvironmentRequest{
		Name: "Production",
		Type: "development",
	}

	resp, err := s.environmentService.CreateEnvironment(s.ctx, req)
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(req.Name, resp.Name)
}
func (s *EnvironmentServiceSuite) TestGetEnvironmentByID() {
	env := &environment.Environment{
		ID:   "env-1",
		Name: "Testing",
		Type: types.EnvironmentDevelopment,
	}

	_ = s.environmentRepo.Create(s.ctx, env)

	// Test retrieval
	resp, err := s.environmentService.GetEnvironment(s.ctx, "env-1")
	s.NoError(err)
	s.NotNil(resp)
	s.Equal(env.Name, resp.Name)

	// Test non-existent environment
	resp, err = s.environmentService.GetEnvironment(s.ctx, "non-existent")
	s.Error(err)
	s.Nil(resp)
}

func (s *EnvironmentServiceSuite) TestListEnvironments() {
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-1", Name: "Production", Type: types.EnvironmentProduction})
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-2", Name: "Development", Type: types.EnvironmentDevelopment})
	_ = s.environmentRepo.Create(s.ctx, &environment.Environment{ID: "env-deleted", Name: "Deleted", Type: types.EnvironmentDevelopment, BaseModel: types.BaseModel{Status: types.StatusDeleted}})

	resp, err := s.environmentService.GetEnvironments(s.ctx, types.Filter{Offset: 0, Limit: 10})
	s.NoError(err)
	s.Len(resp.Environments, 2)

	resp, err = s.environmentService.GetEnvironments(s.ctx, types.Filter{Offset: 10, Limit: 10})
	s.NoError(err)
	s.Len(resp.Environments, 0)
}

func (s *EnvironmentServiceSuite) TestUpdateEnvironment() {
	env := &environment.Environment{
		ID:   "env-1",
		Name: "Development",
		Type: types.EnvironmentDevelopment,
	}
	_ = s.environmentRepo.Create(s.ctx, env)

	// Name updates and an unchanged type should succeed; type stays as it was.
	resp, err := s.environmentService.UpdateEnvironment(s.ctx, "env-1", dto.UpdateEnvironmentRequest{
		Name: "Updated Development",
		Type: string(types.EnvironmentDevelopment),
	})
	s.NoError(err)
	s.NotNil(resp)
	s.Equal("Updated Development", resp.Name)
	s.Equal(string(types.EnvironmentDevelopment), resp.Type)

	// Omitting type should also work and leave the type intact.
	resp, err = s.environmentService.UpdateEnvironment(s.ctx, "env-1", dto.UpdateEnvironmentRequest{
		Name: "Renamed Again",
	})
	s.NoError(err)
	s.Equal(string(types.EnvironmentDevelopment), resp.Type)

	// Attempting to change the type must be rejected.
	_, err = s.environmentService.UpdateEnvironment(s.ctx, "env-1", dto.UpdateEnvironmentRequest{
		Type: string(types.EnvironmentProduction),
	})
	s.Error(err)
}

// Regression: the update must be authorised against the environment named in
// the path. The caller's selected environment is not the target and is absent
// entirely when the request carries no X-Environment-ID, so a check tied to it
// let a user mutate an environment it had no access to.
func (s *EnvironmentServiceSuite) TestUpdateEnvironmentDeniesUnauthorisedTarget() {
	cfg := &config.Configuration{
		EnvAccess: config.EnvAccessConfig{
			UserEnvMapping: map[string]map[string][]string{
				"t_tenant1": {"usr_dev": {"env_dev"}},
			},
		},
	}
	s.environmentService.envAccessService = NewEnvAccessService(cfg)

	ctx := context.WithValue(context.Background(), types.CtxTenantID, "t_tenant1")
	ctx = context.WithValue(ctx, types.CtxUserID, "usr_dev")

	_ = s.environmentRepo.Create(ctx, &environment.Environment{ID: "env_dev", Name: "Development", Type: types.EnvironmentDevelopment})
	_ = s.environmentRepo.Create(ctx, &environment.Environment{ID: "env_prod", Name: "Production", Type: types.EnvironmentProduction})

	// The environment the user may reach is still writable.
	resp, err := s.environmentService.UpdateEnvironment(ctx, "env_dev", dto.UpdateEnvironmentRequest{Name: "Renamed Dev"})
	s.NoError(err)
	s.Equal("Renamed Dev", resp.Name)

	// The one it may not reach is refused, and the record is left untouched.
	_, err = s.environmentService.UpdateEnvironment(ctx, "env_prod", dto.UpdateEnvironmentRequest{Name: "cross-env-write"})
	s.Error(err)
	s.True(ierr.IsPermissionDenied(err))

	unchanged, err := s.environmentRepo.Get(ctx, "env_prod")
	s.NoError(err)
	s.Equal("Production", unchanged.Name)
}

func brandingPNG(s *EnvironmentServiceSuite, w, h int) []byte {
	var buf bytes.Buffer
	s.Require().NoError(png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))))
	return buf.Bytes()
}

func brandingJPEG(s *EnvironmentServiceSuite, w, h int) []byte {
	var buf bytes.Buffer
	s.Require().NoError(jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil))
	return buf.Bytes()
}

// brandingPNGDeclaring returns a tiny PNG whose header claims w x h pixels.
func brandingPNGDeclaring(s *EnvironmentServiceSuite, w, h uint32) []byte {
	data := brandingPNG(s, 1, 1)
	binary.BigEndian.PutUint32(data[16:20], w)
	binary.BigEndian.PutUint32(data[20:24], h)
	binary.BigEndian.PutUint32(data[29:33], crc32.ChecksumIEEE(data[12:29]))
	return data
}

func (s *EnvironmentServiceSuite) TestProcessBrandingImage() {
	exifSegment := append([]byte{0xFF, 0xE1, 0x00, 0x10}, []byte("Exif\x00\x00GPS-LOCATION")...)
	jpegWithExif := append([]byte{0xFF, 0xD8}, append(exifSegment, brandingJPEG(s, 200, 200)[2:]...)...)
	var gif89 bytes.Buffer
	s.Require().NoError(gif.Encode(&gif89, image.NewRGBA(image.Rect(0, 0, 200, 200)), nil))

	tests := []struct {
		name    string
		data    []byte
		asset   types.BrandingAsset
		wantErr string
		wantExt string
	}{
		{name: "wide logo at minimum height", data: brandingPNG(s, 600, 128), asset: types.BrandingAssetLogo, wantExt: "png"},
		{name: "square icon", data: brandingPNG(s, 128, 128), asset: types.BrandingAssetIcon, wantExt: "png"},
		{name: "jpeg stays jpeg", data: brandingJPEG(s, 300, 200), asset: types.BrandingAssetLogo, wantExt: "jpg"},
		{name: "non-square icon", data: brandingPNG(s, 200, 100), asset: types.BrandingAssetIcon, wantErr: "Icon must be square"},
		{name: "small icon", data: brandingPNG(s, 100, 100), asset: types.BrandingAssetIcon, wantErr: "Icon must be at least 128 x 128 px"},
		{name: "short logo", data: brandingPNG(s, 600, 100), asset: types.BrandingAssetLogo, wantErr: "Logo must be at least 128 px on each side"},
		{name: "gif", data: gif89.Bytes(), asset: types.BrandingAssetLogo, wantErr: "Only PNG and JPEG images are allowed"},
		{name: "over 512 KB", data: make([]byte, 513*1024), asset: types.BrandingAssetLogo, wantErr: "Image must be 512 KB or smaller"},
		{name: "declares too many pixels", data: brandingPNGDeclaring(s, 20000, 20000), asset: types.BrandingAssetLogo, wantErr: "Image must be 4 megapixels or smaller"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			img, err := processBrandingImage(tt.data, tt.asset)
			if tt.wantErr != "" {
				s.Require().Error(err)
				s.True(ierr.IsValidation(err))
				s.Contains(err.Error(), tt.wantErr)
				return
			}
			s.Require().NoError(err)
			s.Equal(tt.wantExt, img.Ext())
			s.Len(img.Hash(), 64)
		})
	}

	s.Run("re-encode keeps size and drops metadata", func() {
		img, err := processBrandingImage(jpegWithExif, types.BrandingAssetIcon)
		s.Require().NoError(err)
		s.NotContains(string(img.Data()), "Exif")
		cfg, _, err := image.DecodeConfig(bytes.NewReader(img.Data()))
		s.Require().NoError(err)
		s.Equal(200, cfg.Width)
		s.Equal(200, cfg.Height)
	})

	s.Run("same input gives same hash", func() {
		data := brandingPNG(s, 300, 150)
		a, err := processBrandingImage(data, types.BrandingAssetLogo)
		s.Require().NoError(err)
		b, err := processBrandingImage(data, types.BrandingAssetLogo)
		s.Require().NoError(err)
		s.Equal(a.Hash(), b.Hash())
		s.Equal("tenant_1/env_1/"+a.Hash()+".png", publicAssetKey("tenant_1", "env_1", a))
	})
}

func (s *EnvironmentServiceSuite) TestParsePublicAssetPath() {
	hash := strings.Repeat("ab", 32)
	tests := []struct {
		name, tenant, env, file string
		ok                      bool
	}{
		{name: "prefixed ids", tenant: "tenant_01M77", env: "env_01M77", file: hash + ".png", ok: true},
		{name: "jpg", tenant: "tenant_1", env: "env_1", file: hash + ".jpg", ok: true},
		{name: "self-hosted default tenant", tenant: "00000000-0000-0000-0000-000000000000", env: "00000000-0000-0000-0000-000000000000", file: hash + ".png", ok: true},
		{name: "path traversal", tenant: "..", env: "env_1", file: hash + ".png"},
		{name: "short hash", tenant: "tenant_1", env: "env_1", file: hash[:63] + ".png"},
		{name: "uppercase hash", tenant: "tenant_1", env: "env_1", file: strings.ToUpper(hash) + ".png"},
		{name: "gif extension", tenant: "tenant_1", env: "env_1", file: hash + ".gif"},
		{name: "id over 50 chars", tenant: strings.Repeat("t", 51), env: "env_1", file: hash + ".png"},
		{name: "empty env", tenant: "tenant_1", env: "", file: hash + ".png"},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			key, ext, gotHash, ok := parsePublicAssetPath(tt.tenant, tt.env, tt.file)
			s.Equal(tt.ok, ok)
			if tt.ok {
				s.Equal(tt.tenant+"/"+tt.env+"/"+tt.file, key)
				s.Equal(tt.file[65:], ext)
				s.Equal(hash, gotHash)
			}
		})
	}
}

func (s *EnvironmentServiceSuite) TestSelectBrandingKeys() {
	envLogo, envIcon := lo.ToPtr("t/e/envlogo.png"), lo.ToPtr("t/e/envicon.png")
	entLogo, entIcon := lo.ToPtr("t/e/entlogo.png"), lo.ToPtr("t/e/enticon.png")
	env := &types.BrandingConfig{LogoKey: envLogo, IconKey: envIcon}

	tests := []struct {
		name               string
		entity             *billingentity.BillingEntity
		env                *types.BrandingConfig
		wantLogo, wantIcon *string
	}{
		{name: "entity icon only never mixes in env logo", entity: &billingentity.BillingEntity{IconKey: entIcon}, env: env, wantIcon: entIcon},
		{name: "entity with both", entity: &billingentity.BillingEntity{LogoKey: entLogo, IconKey: entIcon}, env: env, wantLogo: entLogo, wantIcon: entIcon},
		{name: "entity with none uses env", entity: &billingentity.BillingEntity{}, env: env, wantLogo: envLogo, wantIcon: envIcon},
		{name: "no entity uses env", env: env, wantLogo: envLogo, wantIcon: envIcon},
		{name: "nothing anywhere", env: &types.BrandingConfig{}},
		{name: "no env config", entity: &billingentity.BillingEntity{}},
	}
	for _, tt := range tests {
		s.Run(tt.name, func() {
			logo, icon := selectBrandingKeys(tt.entity, tt.env)
			s.Equal(tt.wantLogo, logo)
			s.Equal(tt.wantIcon, icon)
		})
	}
}

func (s *EnvironmentServiceSuite) TestSaveBrandingKeyKeepsTheOtherAsset() {
	env := &environment.Environment{ID: "env_branding", Name: "Sandbox", Type: types.EnvironmentDevelopment}
	env.TenantID = "test-tenant-id"
	ctx := types.SetEnvironmentID(s.ctx, env.ID)
	logo, icon := lo.ToPtr("t/e/logo.png"), lo.ToPtr("t/e/icon.png")

	_, err := s.environmentService.saveBrandingKey(ctx, env, types.BrandingAssetLogo, logo)
	s.Require().NoError(err)
	_, err = s.environmentService.saveBrandingKey(ctx, env, types.BrandingAssetIcon, icon)
	s.Require().NoError(err)
	cfg, err := environmentBranding(ctx, s.environmentService.SettingsRepo, env.ID)
	s.Require().NoError(err)
	s.Equal(logo, cfg.LogoKey)
	s.Equal(icon, cfg.IconKey)

	_, err = s.environmentService.saveBrandingKey(ctx, env, types.BrandingAssetLogo, nil)
	s.Require().NoError(err)
	cfg, err = environmentBranding(ctx, s.environmentService.SettingsRepo, env.ID)
	s.Require().NoError(err)
	s.Nil(cfg.LogoKey)
	s.Equal(icon, cfg.IconKey)
}
