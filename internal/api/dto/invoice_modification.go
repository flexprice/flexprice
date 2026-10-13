package dto

import (
	ierr "github.com/flexprice/flexprice/internal/errors"
)

type InvoiceModifyType string

const (
	InvoiceModifyTypeLineItem InvoiceModifyType = "line_item"
)

// ExecuteInvoiceModifyRequest: exactly one of the *Params fields must be set, matching Type.
type ExecuteInvoiceModifyRequest struct {
	Type           InvoiceModifyType            `json:"type" validate:"required"`
	LineItemParams *InvoiceModifyLineItemParams `json:"line_item_params,omitempty"`
}

func (r *ExecuteInvoiceModifyRequest) Validate() error {
	switch r.Type {
	case InvoiceModifyTypeLineItem:
		if r.LineItemParams == nil {
			return ierr.NewError("line_item_params is required for type 'line_item'").
				Mark(ierr.ErrValidation)
		}
		return r.LineItemParams.Validate()
	default:
		return ierr.NewError("unknown modification type: " + string(r.Type)).
			WithHint("valid values: line_item").
			Mark(ierr.ErrValidation)
	}
}

type InvoiceModifyLineItemAction string

const (
	InvoiceModifyLineItemActionAdd    InvoiceModifyLineItemAction = "add"
	InvoiceModifyLineItemActionUpdate InvoiceModifyLineItemAction = "update"
	InvoiceModifyLineItemActionRemove InvoiceModifyLineItemAction = "remove"
)

type InvoiceModifyLineItemParams struct {
	Action InvoiceModifyLineItemAction `json:"action" validate:"required"`
	// Required for action 'add'. Must contain at least one line item.
	Items []AddLineItemRequest `json:"items,omitempty" validate:"omitempty,min=1"`
	// Required for action 'remove'. Must contain at least one line item ID.
	LineItemIDs []string `json:"line_item_ids,omitempty" validate:"omitempty,min=1"`
	// Required for action 'update' (one line item per call; edits are versioned, so the id changes).
	LineItemID string                 `json:"line_item_id,omitempty"`
	Update     *UpdateLineItemRequest `json:"update,omitempty"`
}

func (p *InvoiceModifyLineItemParams) Validate() error {
	switch p.Action {
	case InvoiceModifyLineItemActionAdd:
		if len(p.Items) == 0 {
			return ierr.NewError("items is required for action 'add'").
				WithHint("provide at least one line item to add").
				Mark(ierr.ErrValidation)
		}
	case InvoiceModifyLineItemActionUpdate:
		if p.LineItemID == "" {
			return ierr.NewError("line_item_id is required for action 'update'").
				WithHint("provide the id of the line item to update").
				Mark(ierr.ErrValidation)
		}
		if p.Update == nil {
			return ierr.NewError("update is required for action 'update'").
				WithHint("provide the fields to update").
				Mark(ierr.ErrValidation)
		}
		if err := p.Update.Validate(); err != nil {
			return err
		}
	case InvoiceModifyLineItemActionRemove:
		if len(p.LineItemIDs) == 0 {
			return ierr.NewError("line_item_ids is required for action 'remove'").
				WithHint("provide at least one line item id to remove").
				Mark(ierr.ErrValidation)
		}
	default:
		return ierr.NewError("unknown line item action: " + string(p.Action)).
			WithHint("valid values: add, update, remove").
			Mark(ierr.ErrValidation)
	}
	return nil
}

type InvoiceModifyResponse struct {
	Invoice *InvoiceResponse `json:"invoice"`
}
