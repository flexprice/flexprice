/**
 * FlexPrice Customer Portal - Dashboard API (custom)
 *
 * Merged from api/custom/typescript/. Uses the Speakeasy-generated
 * FlexPrice SDK (customers, subscriptions, invoices, wallets, entitlements, features).
 */

import type { SDKOptions } from "../lib/config.js";
import type * as shared from "./models/shared/index.js";
import { FlexPrice } from "./sdk.js";

export type DashboardOptions = {
  subscriptionLimit?: number;
  invoiceLimit?: number;
  days?: number;
  startDate?: string;
  endDate?: string;
  includeCustomer?: boolean;
  includeSubscriptions?: boolean;
  includeInvoices?: boolean;
  includeUsage?: boolean;
  includeEntitlements?: boolean;
  includeSummary?: boolean;
  includeWalletBalance?: boolean;
};

export interface CustomerDashboardData {
  customer?: shared.DtoCustomerResponse;
  usage?: shared.DtoCustomerUsageSummaryResponse;
  entitlements?: shared.DtoCustomerEntitlementsResponse;
  walletBalance?: shared.DtoWalletResponse;
  activeSubscriptions?: shared.DtoSubscriptionResponse[];
  invoices?: shared.DtoInvoiceResponse[];
  summary?: shared.DtoCustomerMultiCurrencyInvoiceSummary;
  metadata: {
    fetchedAt: string;
    customerId: string;
    totalSubscriptions?: number;
    totalInvoices?: number;
    errors?: string[];
    warnings?: string[];
  };
}

/**
 * Customer Portal – single entry point for customer dashboard data using the FlexPrice SDK.
 */
export class CustomerPortal {
  private sdk: FlexPrice;

  constructor(options: SDKOptions) {
    this.sdk = new FlexPrice(options);
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
      this.sdk.customers.getCustomerByExternalId({ externalId: customerExternalId }),
    );

    const customerData =
      customer && "id" in customer && customer.id ? customer : undefined;
    if (!customerData?.id) {
      return {
        metadata: {
          fetchedAt: now,
          customerId: customerExternalId,
          ...(errors.length ? { errors } : { errors: [`Customer not found: ${customerExternalId}`] }),
        },
      };
    }

    const customerId = customerData.id as string;

    const [usage, entitlements, walletBalance, subsResp, invoicesResp, summary] = await Promise.all([
      opts.includeUsage
        ? safe("Usage", () =>
            this.sdk.customers.getCustomerUsageSummary({ customerId }),
          )
        : undefined,
      opts.includeEntitlements
        ? safe("Entitlements", () =>
            this.sdk.customers.getCustomerEntitlements({ id: customerId }),
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
            this.sdk.invoices.getCustomerInvoiceSummary({ id: customerId }),
          )
        : undefined,
    ]);

    const activeSubscriptions =
      subsResp && "items" in subsResp ? subsResp.items ?? [] : [];
    const invoices =
      invoicesResp && "items" in invoicesResp ? invoicesResp.items ?? [] : [];

    const isSuccess = <T>(r: T): r is Exclude<T, { error?: unknown }> =>
      !r || !("error" in (r as object));

    return {
      ...(opts.includeCustomer && customerData ? { customer: customerData } : {}),
      ...(usage && isSuccess(usage) ? { usage } : {}),
      ...(entitlements && isSuccess(entitlements) ? { entitlements } : {}),
      ...(walletBalance && isSuccess(walletBalance) ? { walletBalance } : {}),
      ...(activeSubscriptions.length ? { activeSubscriptions } : {}),
      ...(invoices.length ? { invoices } : {}),
      ...(summary && isSuccess(summary) ? { summary } : {}),
      metadata: {
        fetchedAt: now,
        customerId: customerExternalId,
        totalSubscriptions: activeSubscriptions.length,
        totalInvoices: invoices.length,
        ...(errors.length ? { errors } : {}),
        ...(warnings.length ? { warnings } : {}),
      },
    };
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
