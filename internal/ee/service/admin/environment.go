package admin

import (
	"context"

	admindto "github.com/flexprice/flexprice/internal/api/dto/admin"
	"github.com/flexprice/flexprice/internal/ee/service"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// EnvironmentService creates environments for the admin portal.
// It does not apply the self-serve environment quota used by the public API.
type EnvironmentService interface {
	CreateEnvironment(ctx context.Context, req admindto.CreateEnvironmentRequest) (*admindto.EnvironmentResponse, error)
}

type environmentService struct {
	service.ServiceParams
}

func NewEnvironmentService(params service.ServiceParams) EnvironmentService {
	return &environmentService{ServiceParams: params}
}

func (s *environmentService) CreateEnvironment(ctx context.Context, req admindto.CreateEnvironmentRequest) (*admindto.EnvironmentResponse, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}

	tenantID, userID, err := s.resolveTenant(ctx, req.TenantID, req.Email)
	if err != nil {
		return nil, err
	}

	ctx = types.SetTenantID(ctx, tenantID)
	if userID != "" {
		ctx = types.SetUserID(ctx, userID)
	}

	env := req.ToEnvironment(ctx)
	if env == nil {
		return nil, ierr.NewError("failed to build environment").Mark(ierr.ErrInternal)
	}
	if err := s.EnvironmentRepo.Create(ctx, env); err != nil {
		return nil, err
	}

	if s.Logger != nil {
		s.Logger.Info(ctx, "created environment", "environment_id", env.ID, "tenant_id", tenantID, "type", env.Type.String())
	}
	return admindto.NewEnvironmentResponse(env), nil
}

func (s *environmentService) resolveTenant(ctx context.Context, tenantID, email string) (string, string, error) {
	var userID string
	if email != "" {
		u, err := s.UserRepo.GetByEmail(ctx, email)
		if err != nil {
			return "", "", err
		}
		if u == nil || u.TenantID == "" {
			return "", "", ierr.NewError("user has no tenant").
				WithHint("The user for this email is not assigned to a tenant").
				Mark(ierr.ErrValidation)
		}
		if tenantID != "" && u.TenantID != tenantID {
			return "", "", ierr.NewError("email does not belong to the given tenant").
				WithHint("tenant_id and email must refer to the same tenant").
				Mark(ierr.ErrValidation)
		}
		tenantID = u.TenantID
		userID = u.ID
	}

	if _, err := s.TenantRepo.GetByID(ctx, tenantID); err != nil {
		return "", "", err
	}
	return tenantID, userID, nil
}
