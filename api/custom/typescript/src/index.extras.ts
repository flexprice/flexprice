/*
 * Re-export all Speakeasy-generated SDK modules from the root so consumers can do:
 *   import { Flexprice, FlexPriceError, CreateCustomerRequest } from "@flexprice/sdk";
 * All models (shared, errors, operations, callbacks, webhooks) are re-exported from a single models index.
 * Customer Portal (custom) is also re-exported for dashboard aggregation.
 * Short-name aliases for generated enum/const types (Speakeasy uses long GithubCom... names).
 */
export * from "./sdk/models/index.js";
export {
  CustomerPortal,
  createCustomerPortal,
  getCustomerDashboardData,
  type DashboardOptions,
  type CustomerDashboardData,
} from "./sdk/customer-portal.js";