package cmd

import "github.com/spf13/cobra"

// cobra panics at Execute() on an ID that was never registered, so these must
// stay in step with commandGroups. TestEveryGroupIDIsRegistered enforces it.
const (
	groupSetup      = "setup"
	groupCoreBill   = "core-billing"
	groupUsage      = "usage"
	groupCredits    = "credits"
	groupCatalog    = "catalog"
	groupPlatform   = "platform"
	groupAutomation = "automation"
	groupAdvanced   = "advanced"
)

// cobra prints groups in the order added, so this slice is the help layout.
var commandGroups = []*cobra.Group{
	{ID: groupSetup, Title: "Setup"},
	{ID: groupCoreBill, Title: "Core billing"},
	{ID: groupUsage, Title: "Usage & metering"},
	{ID: groupCredits, Title: "Credits & discounts"},
	{ID: groupCatalog, Title: "Catalog & pricing"},
	{ID: groupPlatform, Title: "Platform"},
	{ID: groupAutomation, Title: "Automation"},
	{ID: groupAdvanced, Title: "Advanced"},
}

// Descriptions are hand-written: the spec's summaries describe individual
// operations ("Get customer by external ID"), not the resource, so deriving a
// parent from them is misleading.
type resourceEntry struct {
	GroupID string
	Short   string
}

// A missing resource still appears under cobra's "Additional Commands" rather
// than being dropped, and TestEveryResourceHasAGroup fails so CI catches it.
var resourceGroups = map[string]resourceEntry{
	// Core billing
	"customers":               {groupCoreBill, "Manage the people and organisations you bill"},
	"subscriptions":           {groupCoreBill, "Active plan assignments and their lifecycle"},
	"subscription-schedules":  {groupCoreBill, "Planned future changes to a subscription"},
	"subscription-line-items": {groupCoreBill, "Individual charges on a subscription"},
	"invoices":                {groupCoreBill, "Draft, finalize and void billing documents"},
	"payments":                {groupCoreBill, "Payment attempts and their outcomes"},
	"checkout":                {groupCoreBill, "Hosted checkout sessions"},

	// Usage & metering
	"events":       {groupUsage, "Raw usage events you send in for metering"},
	"features":     {groupUsage, "Capabilities that can be metered or gated"},
	"entitlements": {groupUsage, "What a customer's plan grants them access to"},
	"costs":        {groupUsage, "Cost sheets derived from usage"},

	// Credits & discounts
	"credit-grants":       {groupCredits, "Prepaid and promotional credit allocations"},
	"credit-notes":        {groupCredits, "Refunds and credit memos against invoices"},
	"wallets":             {groupCredits, "Prepaid credit balances held by a customer"},
	"coupons":             {groupCredits, "Discount codes and their rules"},
	"coupon-associations": {groupCredits, "Which coupons apply to which subscriptions"},

	// Catalog & pricing
	"plans":            {groupCatalog, "Pricing models customers can subscribe to"},
	"prices":           {groupCatalog, "Individual pricing units within a plan"},
	"price-units":      {groupCatalog, "Units of measurement used by prices"},
	"addons":           {groupCatalog, "Optional extras attachable to a plan"},
	"tax-rates":        {groupCatalog, "Tax rates available to apply"},
	"tax-associations": {groupCatalog, "Which tax rates apply to which entities"},

	// Platform
	"environments": {groupPlatform, "Isolated spaces within your tenant"},
	"secrets":      {groupPlatform, "API keys and integration credentials"},
	"users":        {groupPlatform, "People with access to your tenant"},
	"tenants":      {groupPlatform, "Your top-level account"},
	"rbac":         {groupPlatform, "Roles and permissions"},
	"groups":       {groupPlatform, "Collections of users or entities"},
	"integrations": {groupPlatform, "Connections to Stripe, HubSpot and others"},

	// Automation
	"workflows":       {groupAutomation, "Long-running billing processes"},
	"tasks":           {groupAutomation, "Background jobs and their status"},
	"scheduled-tasks": {groupAutomation, "Work queued to run later"},
	"alerts":          {groupAutomation, "Threshold and anomaly notifications"},
	"alert-settings":  {groupAutomation, "How and when alerts fire"},
}

// Hand-written commands, kept separate because they are not spec-derived and
// so are not covered by TestEveryResourceHasAGroup.
var builtinGroups = map[string]string{
	"init": groupSetup, "login": groupSetup, "logout": groupSetup,
	"whoami": groupSetup, "env": groupSetup, "config": groupSetup,
	"open": groupSetup, "version": groupSetup,

	"get": groupAdvanced, "post": groupAdvanced, "delete": groupAdvanced,
	"resources": groupAdvanced,
}
