package dto

// Define any necessary types or functions here

import "time"

type GenerateInvoiceRequest struct {
	// Define the fields required for generating an invoice
	CustomerID     string
	SubscriptionID string
	PeriodStart    time.Time
	PeriodEnd      time.Time
}

type CalculatePriceRequest struct {
	// Define the fields required for calculating price
	CustomerID     string
	SubscriptionID string
	UsageData      interface{}
}
