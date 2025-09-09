package service

import (
	"context"
	"fmt"

	"github.com/flexprice/flexprice/internal/api/dto"
	ierr "github.com/flexprice/flexprice/internal/errors"
	"github.com/flexprice/flexprice/internal/types"
)

// PaymentGatewayService provides generic payment gateway operations
type PaymentGatewayService interface {
	// CreatePaymentLink creates a payment link using the specified or preferred gateway
	CreatePaymentLink(ctx context.Context, req *dto.CreatePaymentLinkRequest) (*dto.PaymentLinkResponse, error)

	// GetPaymentStatus retrieves payment status from any gateway by session ID
	GetPaymentStatus(ctx context.Context, sessionID string) (*dto.GenericPaymentStatusResponse, error)

	// GetSupportedGateways returns all supported payment gateways for the environment
	GetSupportedGateways(ctx context.Context) (*dto.GetSupportedGatewaysResponse, error)
}

// paymentGatewayService implements PaymentGatewayService
type paymentGatewayService struct {
	ServiceParams
	factory *PaymentGatewayFactory
}

// NewPaymentGatewayService creates a new payment gateway service
func NewPaymentGatewayService(params ServiceParams) PaymentGatewayService {
	factory := NewPaymentGatewayFactory(params)

	return &paymentGatewayService{
		ServiceParams: params,
		factory:       factory,
	}
}

// CreatePaymentLink creates a payment link using the specified or preferred gateway
func (s *paymentGatewayService) CreatePaymentLink(ctx context.Context, req *dto.CreatePaymentLinkRequest) (*dto.PaymentLinkResponse, error) {
	s.Logger.Infow("creating payment link",
		"invoice_id", req.InvoiceID,
		"customer_id", req.CustomerID,
		"amount", req.Amount.String(),
		"currency", req.Currency,
		"requested_gateway", req.Gateway,
	)

	// Validate request
	if err := req.Validate(); err != nil {
		return nil, ierr.WithError(err).
			WithHint("Invalid payment link request").
			Mark(ierr.ErrValidation)
	}

	// Determine which gateway to use
	var gatewayType types.PaymentGatewayType
	var err error

	if req.Gateway != nil {
		gatewayType = *req.Gateway
		s.Logger.Infow("using requested gateway", "gateway", gatewayType)
	} else {
		gatewayType, err = s.factory.GetPreferredGateway(ctx)
		if err != nil {
			return nil, err
		}
		s.Logger.Infow("using preferred gateway", "gateway", gatewayType)
	}

	// Get the appropriate gateway service
	gatewayService, err := s.factory.GetGateway(ctx, gatewayType)
	if err != nil {
		return nil, err
	}

	// Convert to the appropriate request type and call the gateway service
	switch gatewayType {
	case types.PaymentGatewayTypeStripe:
		stripeService := gatewayService.(*StripeService)

		// Convert generic request to Stripe-specific request
		stripeReq := &dto.CreateStripePaymentLinkRequest{
			InvoiceID:              req.InvoiceID,
			CustomerID:             req.CustomerID,
			Amount:                 req.Amount,
			Currency:               req.Currency,
			SuccessURL:             req.SuccessURL,
			CancelURL:              req.CancelURL,
			Metadata:               req.Metadata,
			SaveCardAndMakeDefault: req.SaveCardAndMakeDefault,
		}

		// Get environment ID from context
		environmentID := types.GetEnvironmentID(ctx)
		if environmentID == "" {
			return nil, ierr.NewError("environment not found in context").
				WithHint("Request context must contain environment_id").
				Mark(ierr.ErrValidation)
		}
		stripeReq.EnvironmentID = environmentID

		stripeResp, err := stripeService.CreatePaymentLink(ctx, stripeReq)
		if err != nil {
			return nil, err
		}

		// Convert Stripe response to generic response
		response := &dto.PaymentLinkResponse{
			ID:              stripeResp.ID,
			PaymentURL:      stripeResp.PaymentURL,
			PaymentIntentID: stripeResp.PaymentIntentID,
			Amount:          stripeResp.Amount,
			Currency:        stripeResp.Currency,
			Status:          stripeResp.Status,
			CreatedAt:       stripeResp.CreatedAt,
			PaymentID:       stripeResp.PaymentID,
			Gateway:         string(types.PaymentGatewayTypeStripe),
		}

		return response, nil

	default:
		return nil, ierr.NewError("gateway not supported").
			WithHint(fmt.Sprintf("Gateway type '%s' is not supported for payment link creation", gatewayType)).
			WithReportableDetails(map[string]interface{}{
				"gateway_type": gatewayType,
			}).
			Mark(ierr.ErrValidation)
	}
}

// GetPaymentStatus retrieves payment status from any gateway by session ID
func (s *paymentGatewayService) GetPaymentStatus(ctx context.Context, sessionID string) (*dto.GenericPaymentStatusResponse, error) {
	s.Logger.Infow("getting payment status", "session_id", sessionID)

	// First, try to find the payment record to determine which gateway was used
	paymentService := NewPaymentService(s.ServiceParams)
	filter := types.NewNoLimitPaymentFilter()
	// Set a reasonable limit for searching
	if filter.QueryFilter != nil {
		limit := 50
		filter.QueryFilter.Limit = &limit
	}

	payments, err := paymentService.ListPayments(ctx, filter)
	if err != nil {
		s.Logger.Errorw("failed to list payments to find gateway", "error", err)
		// Continue with trying all gateways
	}

	var gatewayType *types.PaymentGatewayType
	if payments != nil {
		// Find payment with matching session ID
		for _, paymentResp := range payments.Items {
			if paymentResp.GatewayPaymentID != nil && *paymentResp.GatewayPaymentID == sessionID {
				if paymentResp.PaymentGateway != nil {
					switch *paymentResp.PaymentGateway {
					case "stripe":
						gatewayType = &[]types.PaymentGatewayType{types.PaymentGatewayTypeStripe}[0]
					case "razorpay":
						gatewayType = &[]types.PaymentGatewayType{types.PaymentGatewayTypeRazorpay}[0]
						// Add more gateway mappings as needed
					}
					break
				}
			}
		}
	}

	// If we found the gateway type, use it directly
	if gatewayType != nil {
		gatewayService, err := s.factory.GetGateway(ctx, *gatewayType)
		if err != nil {
			return nil, err
		}

		switch *gatewayType {
		case types.PaymentGatewayTypeStripe:
			stripeService := gatewayService.(*StripeService)
			environmentID := types.GetEnvironmentID(ctx)
			if environmentID == "" {
				return nil, ierr.NewError("environment not found in context").
					WithHint("Request context must contain environment_id").
					Mark(ierr.ErrValidation)
			}

			stripeResp, err := stripeService.GetPaymentStatus(ctx, sessionID, environmentID)
			if err != nil {
				return nil, err
			}

			// Convert Stripe response to generic response
			response := &dto.GenericPaymentStatusResponse{
				SessionID:       stripeResp.SessionID,
				PaymentIntentID: stripeResp.PaymentIntentID,
				Status:          stripeResp.Status,
				Amount:          stripeResp.Amount,
				Currency:        stripeResp.Currency,
				CustomerID:      stripeResp.CustomerID,
				CreatedAt:       stripeResp.CreatedAt,
				ExpiresAt:       stripeResp.ExpiresAt,
				Metadata:        stripeResp.Metadata,
				Gateway:         string(types.PaymentGatewayTypeStripe),
			}

			return response, nil
		}
	}

	// Otherwise, try all available gateways
	supportedGateways := s.factory.GetSupportedGateways()
	for _, gwType := range supportedGateways {
		gatewayService, err := s.factory.GetGateway(ctx, gwType)
		if err != nil {
			s.Logger.Debugw("gateway not available", "gateway", gwType, "error", err)
			continue // Skip unavailable gateways
		}

		switch gwType {
		case types.PaymentGatewayTypeStripe:
			stripeService := gatewayService.(*StripeService)
			environmentID := types.GetEnvironmentID(ctx)
			if environmentID == "" {
				continue // Skip if no environment ID
			}

			stripeResp, err := stripeService.GetPaymentStatus(ctx, sessionID, environmentID)
			if err != nil {
				s.Logger.Debugw("failed to get status from gateway", "gateway", gwType, "error", err)
				continue // Try next gateway
			}

			// Successfully got status from this gateway
			s.Logger.Infow("found payment status", "session_id", sessionID, "gateway", gwType)

			response := &dto.GenericPaymentStatusResponse{
				SessionID:       stripeResp.SessionID,
				PaymentIntentID: stripeResp.PaymentIntentID,
				Status:          stripeResp.Status,
				Amount:          stripeResp.Amount,
				Currency:        stripeResp.Currency,
				CustomerID:      stripeResp.CustomerID,
				CreatedAt:       stripeResp.CreatedAt,
				ExpiresAt:       stripeResp.ExpiresAt,
				Metadata:        stripeResp.Metadata,
				Gateway:         string(gwType),
			}

			return response, nil
		}
	}

	// No gateway could find the payment
	return nil, ierr.NewError("payment session not found").
		WithHint("Session ID not found in any configured payment gateway").
		WithReportableDetails(map[string]interface{}{
			"session_id": sessionID,
		}).
		Mark(ierr.ErrNotFound)
}

// GetSupportedGateways returns all supported payment gateways for the environment
func (s *paymentGatewayService) GetSupportedGateways(ctx context.Context) (*dto.GetSupportedGatewaysResponse, error) {
	s.Logger.Infow("getting supported gateways")

	// For now, return all supported gateways
	// In the future, this could check which gateways are configured for the environment
	supportedTypes := s.factory.GetSupportedGateways()

	var gateways []dto.GatewayInfo
	for _, gwType := range supportedTypes {
		// For now, mark Stripe as active and preferred
		isActive := gwType == types.PaymentGatewayTypeStripe
		isPreferred := gwType == types.PaymentGatewayTypeStripe

		gatewayInfo := dto.GatewayInfo{
			Type:        gwType,
			Name:        s.getGatewayDisplayName(gwType),
			IsActive:    isActive,
			IsPreferred: isPreferred,
		}

		gateways = append(gateways, gatewayInfo)
	}

	return &dto.GetSupportedGatewaysResponse{
		Gateways: gateways,
	}, nil
}

// Helper methods

func (s *paymentGatewayService) getGatewayDisplayName(gwType types.PaymentGatewayType) string {
	switch gwType {
	case types.PaymentGatewayTypeStripe:
		return "Stripe"
	case types.PaymentGatewayTypeRazorpay:
		return "Razorpay"
	case types.PaymentGatewayTypeFinix:
		return "Finix"
	default:
		return string(gwType)
	}
}
