# Enum and type naming – what to update in `internal/types` and `internal/api/dto`

To get SDK usage like **`FeatureType.Boolean`**, **`Status.Published`**, **`TaxRateEntityType.Customer`** (similar to Supabase/Resend/Render), two things are needed:

1. **Clean type names** (e.g. `FeatureType` instead of `TypesFeatureType`) → done via **overlay or post-process** on the OpenAPI spec (`x-speakeasy-name-override` on each `types.*` schema). No changes in these Go packages.
2. **Shorter enum value names** (e.g. `Boolean` instead of `FeatureTypeBoolean`) → add **`// @name ShortName`** on enum consts in **`internal/types`** below, then run `make swagger`.

---

## internal/types/ – files and enums to update

Add **`// @name <ShortName>`** on each const in the const blocks for types that appear in the API. Swaggo will then emit shorter `x-enum-varnames` and Speakeasy will generate e.g. `FeatureType.Boolean`, `Status.Published`.

| File | Type (enum) | Consts → add `// @name` |
|------|--------------|--------------------------|
| **feature.go** | `FeatureType` | `FeatureTypeMetered` → `// @name Metered`, `FeatureTypeBoolean` → `// @name Boolean`, `FeatureTypeStatic` → `// @name Static` |
| **status.go** | `Status` | `StatusPublished` → `// @name Published`, `StatusDeleted` → `// @name Deleted`, `StatusArchived` → `// @name Archived` |
| **taxrate.go** | `TaxRateType` | `TaxRateTypePercentage` → `// @name Percentage`, `TaxRateTypeFixed` → `// @name Fixed` |
| **taxrate.go** | `TaxRateScope` | `TaxRateScopeInternal` → `// @name Internal`, `TaxRateScopeExternal` → `// @name External`, `TaxRateScopeOneTime` → `// @name OneTime` |
| **taxrate.go** | `TaxRateEntityType` | `TaxRateEntityTypeCustomer` → `// @name Customer`, `TaxRateEntityTypeSubscription` → `// @name Subscription`, `TaxRateEntityTypeInvoice` → `// @name Invoice`, `TaxRateEntityTypeTenant` → `// @name Tenant` |
| **taxrate.go** | `TaxRateStatus` | `TaxRateStatusActive` → `// @name Active`, `TaxRateStatusInactive` → `// @name Inactive` |
| **taxrate.go** | `TaxRateAssignmentStatus` | `TaxRateAssignmentStatusActive` → `// @name Active`, `TaxRateAssignmentStatusInactive` → `// @name Inactive`, `TaxRateAssignmentStatusSuspended` → `// @name Suspended` |
| **addon.go** | `AddonType` | `AddonTypeOnetime` → `// @name Onetime`, `AddonTypeMultipleInstance` → `// @name MultipleInstance` |
| **addon.go** | `AddonStatus` | `AddonStatusActive` → `// @name Active`, `AddonStatusCancelled` → `// @name Cancelled`, `AddonStatusPaused` → `// @name Paused` |
| **addonassociation.go** | `AddonAssociationEntityType` | (all consts in the block) → short names per value |
| **coupon.go** | `CouponType` | (all consts) → short names |
| **coupon.go** | `CouponCadence` | (all consts) → short names |
| **creditnote.go** | `CreditNoteStatus`, `CreditNoteReason`, `CreditNoteType` | (all consts in each block) → short names |
| **invoice.go** | `InvoiceLineItemEntityType`, `InvoiceCadence`, `InvoiceFlowType`, `InvoiceType`, `InvoiceStatus`, `InvoiceBillingReason`, `InvoiceNumberFormat` | (all consts in each block) → short names |
| **subscription.go** | `InvoiceBilling`, `SubscriptionLineItemEntityType`, `SubscriptionStatus`, `PaymentBehavior`, `CollectionMethod`, `PaymentTerms`, `PauseStatus`, `SubscriptionChangeType`, `SubscriptionScheduleChangeType`, `ScheduleStatus`, `ScheduleType` | (all consts in each block) → short names |
| **payment.go** | `PaymentStatus`, `PaymentMethodType`, `PaymentDestinationType`, … | (all consts) → short names |
| **task.go** | `TaskType`, `EntityType`, `FileType`, `TaskStatus` | (all consts) → short names |
| **proration.go** | `ProrationAction`, `ProrationStrategy`, `ProrationBehavior`, `BillingMode`, `TerminationReason`, `CancellationType`, `CancelImmediatelyInvoicePolicy`, `BillingCycleAnchor` | (all consts) → short names |
| **billing.go** | `InvoiceReferencePoint`, `BillingCycle` | (all consts) → short names |
| **entitlement.go** | `EntitlementUsageResetPeriod`, `EntitlementEntityType` | (all consts) → short names |
| **creditgrant.go** | `CreditGrantScope`, `CreditGrantCadence`, `CreditGrantPeriod`, `CreditGrantExpiryType`, `CreditGrantExpiryDurationUnit` | (all consts) → short names |
| **creditgrantapplication.go** | `ApplicationStatus`, `CreditGrantApplicationReason` | (all consts) → short names |
| **wallet.go** | `WalletStatus`, `WalletType`, `TransactionReason`, `WalletTxReferenceType`, `WalletConfigPriceType`, `CreditExpirySkipReason` | (all consts) → short names |
| **transaction.go** | `TransactionType`, `TransactionStatus` | (all consts) → short names |
| **secret.go** | `SecretType`, `SecretProvider` | (all consts) → short names |
| **event.go** | `FailurePointType`, `DebugTrackerStatus`, `EventProcessingStatusType` | (all consts) → short names |
| **scheduled_task.go** | `ScheduledTaskInterval`, `ScheduledTaskEntityType` | (all consts) → short names |
| **pause_mode.go** | `PauseMode`, `ResumeMode` | (all consts) → short names |
| **alertlogs.go** | `AlertState`, `AlertType`, `AlertEntityType`, `AlertCondition` | (all consts) → short names |
| **search_filter.go** | `DataType`, `FilterOperatorType`, `SortDirection` | (all consts) → short names |
| **price.go** | `BillingModel`, `BillingPeriod`, `BillingCadence`, `BillingTier`, `PriceType`, `PriceScope`, `PriceEntityType`, `PriceUnitType`, `RoundType` | (all consts) → short names |
| **expand.go** | `ExpandableField` | (all consts) → short names if desired |
| **filter.go** | `Order`, `Status` (if different from status.go) | (all consts) → short names |
| **commitment.go** | `CommitmentType` | (all consts) → short names |
| **user.go** | `UserType` | (all consts) → short names |
| **entityintegrationmapping.go** | `IntegrationEntityType` | (all consts) → short names |
| **payment_gateway.go** | `PaymentGatewayType`, `WebhookEventType` | (all consts) → short names |
| **group_entity.go** | `GroupEntityType` | (all consts) → short names |
| **temporal.go** | `TemporalTaskQueue`, `TemporalWorkflowType`, `WorkflowExecutionStatus` | (all consts) → short names |
| **config.go** | `RunMode`, `LogLevel` | (all consts) → short names (only if exposed in API) |
| **auth.go** | `AuthProvider` | (all consts) → short names |
| **oauth.go** | `OAuthProvider` | (all consts) → short names |
| **environment.go** | `EnvironmentType` | (all consts) → short names |
| **reset_usage.go** | `ResetUsage` | (all consts) → short names |
| **window_size.go** | `WindowSize` | (all consts) → short names |
| **typst.go** | `TemplateName` | (all consts) → short names |
| **connection.go** | `ConnectionMetadataType` | (all consts) → short names |
| **publisher.go** | `PublishDestination` | (all consts) → short names |
| **meter.go** | `MeterSortField` | (all consts) → short names |
| **settings.go** | `SettingKey`, `CustomAnalyticsRuleID` | (all consts) → short names (only if in API) |
| **context.go** | `ContextKey` | (all consts) → short names (only if in API) |

**Pattern:** For each const line, append `// @name <ShortName>` where `<ShortName>` is the suffix you want in the SDK (e.g. `Boolean`, `Published`, `Customer`). Keep names unique within that enum type.

---

## internal/types/ – structs (optional)

Structs (e.g. `CustomerFilter`, `FeatureFilter`, `QueryFilter`) currently become OpenAPI schemas like `types.CustomerFilter`. To change the **schema name** in the spec (and thus what Speakeasy sees), you can add **`//@name CustomName`** on the struct (e.g. `//@name CustomerFilter`). This is **optional** if you use **overlay/post-process** to set `x-speakeasy-name-override` on the spec for all `types.*` schemas (e.g. strip the `types.` prefix). Prefer overlay for consistency so you don’t have to annotate every struct.

---

## internal/api/dto/ – what to update

- **No enum type definitions** – DTOs use types from `internal/types` and from the same package; enums are defined only in `internal/types`.
- **Structs:** All DTOs are structs (e.g. `CreateFeatureRequest`, `FeatureResponse`). They already have clear names. You only need to add **`//@name …`** on a DTO struct if you want a **different OpenAPI schema name** (e.g. without the `dto.` prefix). That can also be done in the overlay with `x-speakeasy-name-override` for each `dto.*` schema, so **no changes in `internal/api/dto` are required** for the enum/Supabase-style naming plan unless you specifically want to rename certain request/response schemas in the spec.

**Summary for dto:** No changes required for enum usage. Optionally add `//@name` on structs only where you want to override the generated schema name and are not using overlay for that.

---

## After editing

1. Run **`make swagger`** so Swaggo regenerates the OpenAPI spec with the new `x-enum-varnames` (and any `@name` on structs).
2. Apply your **overlay or post-process** so every `types.*` (and optionally `dto.*`) schema has **`x-speakeasy-name-override`** (e.g. `types.FeatureType` → `FeatureType`).
3. Run **`make sdk-all`** (or your SDK generation).
4. Update **api/tests/** (and any other consumers) to use the new type/value names (e.g. `FeatureType` instead of `TypesFeatureType`, `FeatureType.Boolean`).
