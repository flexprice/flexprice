/**
 * FlexPrice Customer Portal - Dashboard API (custom)
 *
 * Merged from api/custom/typescript/. Uses the FlexPrice SDK
 * (customers, subscriptions, invoices, wallets, entitlements, features).
 */

import type { SDKOptions } from "../lib/config.js";
import { Flexprice } from "../index.js";
import type * as models from "./models/index.js";

export type DashboardOptions = {
  subscriptionLimit?: number;
  invoiceLimit?: number;
  includeCustomer?: boolean;
  includeSubscriptions?: boolean;
  includeInvoices?: boolean;
  includeUsage?: boolean;
  includeEntitlements?: boolean;
  includeSummary?: boolean;
  includeWalletBalance?: boolean;
};

export interface CustomerDashboardData {
  customer?: models.DtoCustomerResponse;
  usage?: models.DtoCustomerUsageSummaryResponse;
  entitlements?: models.DtoCustomerEntitlementsResponse;
  walletBalance?: models.DtoWalletResponse;
  activeSubscriptions?: models.DtoSubscriptionResponse[];
  invoices?: models.DtoInvoiceResponse[];
  summary?: models.DtoCustomerMultiCurrencyInvoiceSummary;
  metadata: {
    fetchedAt: string;
    customerId: string;
    totalSubscriptions?: number;
    totalInvoices?: number;
    errors?: string[];
    warnings?: string[];
  };
}

function isRecord(x: unknown): x is Record<string, unknown> {
  return typeof x === "object" && x !== null;
}

/**
 * Customer Portal – single entry point for customer dashboard data using the FlexPrice SDK.
 */
export class CustomerPortal {
  private sdk: Flexprice;

  constructor(options: SDKOptions) {
    this.sdk = new Flexprice(options);
  }

  /**
   * Get dashboard data for a customer by external ID.
   */
  async getDashboardData(
    customerExternalId: string,
    options: DashboardOptions = {},
  ): Promise<CustomerDashboardData> {
    const opts = {
      subscriptionLimit: 10,
      invoiceLimit: 5,
      includeCustomer: true,
      includeSubscriptions: true,
      includeInvoices: true,
      includeUsage: true,
      includeEntitlements: true,
      includeSummary: true,
      includeWalletBalance: true,
      ...options,
    };

    const errors: string[] = [];
    const warnings: string[] = [];
    const now = new Date().toISOString();

    const safe = async <T>(label: string, fn: () => Promise<T>): Promise<T | undefined> => {
      try {
        return await fn();
      } catch (e) {
        const msg = `${label}: ${e instanceof Error ? e.message : String(e)}`;
        errors.push(msg);
        return undefined;
      }
    };

    const customer = await safe("Customer lookup", () =>
      this.sdk.customers.getCustomerByExternalId(customerExternalId),
    );

    const customerData =
      customer && isRecord(customer) && "id" in customer && customer["id"] ? customer : undefined;
    if (!customerData?.["id"]) {
      return {
        metadata: {
          fetchedAt: now,
          customerId: customerExternalId,
          ...(errors.length ? { errors } : { errors: [`Customer not found: ${customerExternalId}`] }),
        },
      };
    }

    const customerId = customerData["id"] as string;

    const [usage, entitlements, walletBalance, subsResp, invoicesResp, summary] = await Promise.all([
      opts.includeUsage
        ? safe("Usage", () =>
          this.sdk.customers.getCustomerUsageSummary({ customerId }),
        )
        : undefined,
      opts.includeEntitlements
        ? safe("Entitlements", () =>
          this.sdk.customers.getCustomerEntitlements(customerId),
        )
        : undefined,
      opts.includeWalletBalance
        ? safe("Wallets", () =>
          this.sdk.wallets.getCustomerWallets({
            id: customerId,
            includeRealTimeBalance: true,
          }),
        ).then((w) => (Array.isArray(w) && w.length > 0 ? w[0] : undefined))
        : undefined,
      opts.includeSubscriptions
        ? safe("Subscriptions", () =>
          this.sdk.subscriptions.querySubscription({
            customerId,
            externalCustomerId: customerExternalId,
            limit: opts.subscriptionLimit ?? 10,
          }),
        )
        : undefined,
      opts.includeInvoices
        ? safe("Invoices", () =>
          this.sdk.invoices.queryInvoice({
            customerId,
            limit: opts.invoiceLimit ?? 5,
          }),
        )
        : undefined,
      opts.includeSummary
        ? safe("Summary", () =>
          this.sdk.invoices.getCustomerInvoiceSummary(customerId),
        )
        : undefined,
    ]);

    const activeSubscriptions: models.DtoSubscriptionResponse[] =
      subsResp && isRecord(subsResp) && "items" in subsResp && Array.isArray(subsResp["items"])
        ? subsResp["items"]
        : [];
    const invoices: models.DtoInvoiceResponse[] =
      invoicesResp && isRecord(invoicesResp) && "items" in invoicesResp && Array.isArray(invoicesResp["items"])
        ? invoicesResp["items"]
        : [];

    const isSuccess = <T>(r: T): r is Exclude<T, { error?: unknown }> =>
      !r || typeof r !== "object" || r === null || !("error" in r);

    const metadata = {
      fetchedAt: now,
      customerId: customerExternalId,
      totalSubscriptions: activeSubscriptions.length,
      totalInvoices: invoices.length,
      ...(errors.length ? { errors } : {}),
      ...(warnings.length ? { warnings } : {}),
    };

    const result: CustomerDashboardData = { metadata };

    if (opts.includeCustomer && customerData) result.customer = customerData as models.DtoCustomerResponse;
    if (usage && isSuccess(usage)) result.usage = usage;
    if (entitlements && isSuccess(entitlements)) result.entitlements = entitlements;
    if (walletBalance && isSuccess(walletBalance)) result.walletBalance = walletBalance;
    if (activeSubscriptions.length > 0) result.activeSubscriptions = activeSubscriptions;
    if (invoices.length > 0) result.invoices = invoices;
    if (summary && isSuccess(summary)) result.summary = summary;

    return result;
  }
}

export function createCustomerPortal(options: SDKOptions): CustomerPortal {
  return new CustomerPortal(options);
}

export async function getCustomerDashboardData(
  customerExternalId: string,
  options?: DashboardOptions,
  config?: SDKOptions,
): Promise<CustomerDashboardData> {
  if (!config) throw new Error("SDKOptions required (e.g. serverURL, apiKeyAuth)");
  const portal = new CustomerPortal(config);
  return portal.getDashboardData(customerExternalId, options ?? {});
}
