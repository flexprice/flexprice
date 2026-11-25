package sample

// one
// two
// three
// four
// five
// want `CL001: cut this comment to 4 lines or fewer`
func longComment() {}

// ------------------------------------------------------------
// want `CL001: drop the section divider`
func divider() {}

// TODO handle refunds here
// want `CL001: link the FLE ticket or issue that tracks this`
func todoWithoutLink() {}

// TODO(FLE-1234): handle refunds here
func todoWithLink() {}

// Retries twice; the gateway drops the first call after a deploy (see FLE-999).
// want `CL001: drop the ticket reference`
func ticketReference() {}

// quarantine: FLE-1234 flaky until the clock is injected
func quarantineIsFine() {}

// result := compute(x)
//
//	if result > 3 {
//		return result
//	}
//
// want `CL001: delete commented-out code`
func commentedOut() {}

// if the provider omits the currency, keep the invoice currency.
func conditionalProse() {}

// NewLedger creates a new ledger.
// want `CL001: drop the comment; it only repeats the name below it`
func NewLedger() {}

// openLedger reads the row under the tenant lock so a refund cannot race it.
func openLedger() {}

// Copyright 2026 Flexprice
// Licensed under the AGPL, version 3
// You may not use this file except in compliance with the License.
// You may obtain a copy of the License at the repository root.
// Unless required by law, software is distributed on an "AS IS" basis.
func licenseIsFine() {}

//go:generate echo generated
func directiveIsFine() {}

type ledger struct {
	// balance is signed; a refund makes it negative until the wallet settles.
	balance int
	// amountStr: the amount.
	// want `CL001: drop the comment; it only repeats the name below it`
	amountStr string // want "CL007: `amountStr` is named after its type; name it for what it holds, such as `amount`"
}
