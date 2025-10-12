/* tslint:disable */
/* eslint-disable */
/**
 * FlexPrice Customer Portal - Complete Dashboard API
 * 
 * A comprehensive, clean interface for fetching customer dashboard data
 * with customizable options for limits, sorting, and filtering.
 * 
 * This file combines all CustomerPortal functionality into a single, easy-to-use API.
 */

import * as runtime from '../runtime';
import type {
    DtoCustomerResponse,
    DtoCustomerUsageSummaryResponse,
    DtoCustomerEntitlementsResponse,
    DtoWalletBalanceResponse,
    DtoListSubscriptionsResponse,
    DtoListInvoicesResponse,
    DtoCustomerMultiCurrencyInvoiceSummary,
    DtoGetUsageAnalyticsResponse,
    DtoSubscriptionResponse,
    DtoInvoiceResponse,
    DtoEntitlementResponse,
    DtoFeatureResponse,
} from '../models/index';
import {
    SubscriptionsGetSubscriptionStatusEnum,
    InvoicesGetInvoiceStatusEnum,
} from './index';
import { TypesSubscriptionStatus } from '../models/TypesSubscriptionStatus';
import { TypesInvoiceStatus } from '../models/TypesInvoiceStatus';
import { TypesPaymentStatus } from '../models/TypesPaymentStatus';
import { CustomersApi } from './CustomersApi';
import { SubscriptionsApi } from './SubscriptionsApi';
import { InvoicesApi } from './InvoicesApi';
import { WalletsApi } from './WalletsApi';
import { EntitlementsApi } from './EntitlementsApi';
import { FeaturesApi } from './FeaturesApi';

/**
 * API operation types for better type safety
 */
export enum ApiOperationType {
    CUSTOMER_LOOKUP = 'Customer Lookup',
    USAGE = 'Usage',
    ENTITLEMENTS = 'Entitlements',
    WALLET = 'Wallet',
    SUBSCRIPTIONS = 'Subscriptions',
    INVOICES = 'Invoices',
    SUMMARY = 'Summary',
    FEATURES = 'Features'
}


/**
 * Options for dashboard data fetching
 */
export interface DashboardOptions {
    /** Number of subscriptions to fetch (default: 10) */
    subscriptionLimit?: number;
    /** Number of invoices to fetch (default: 5) */
    invoiceLimit?: number;
    /** Subscription status filter (default: ['active']) */
    subscriptionStatus?: SubscriptionsGetSubscriptionStatusEnum[];
    /** Invoice status filter (default: ['finalized']) */
    invoiceStatus?: InvoicesGetInvoiceStatusEnum[];
    /** Time range - last N days */
    days?: number;
    /** Start date for filtering */
    startDate?: string;
    /** End date for filtering */
    endDate?: string;
    /** What to include in the response */
    includeCustomer?: boolean;
    includeSubscriptions?: boolean;
    includeInvoices?: boolean;
    includeUsage?: boolean;
    includeEntitlements?: boolean;
    includeSummary?: boolean;
    includeAnalytics?: boolean;
    includeFeatures?: boolean;
    includeWalletBalance?: boolean;
    /** Additional limits */
    entitlementLimit?: number;
    /** Feature IDs to filter entitlements and usage */
    featureIds?: string[];
    /** Subscription IDs to filter data */
    subscriptionIds?: string[];
}

/**
 * Dashboard data response structure
 */
export interface CustomerDashboardData {
    /** Customer details */
    customer?: DtoCustomerResponse;
    /** Customer usage summary */
    usage?: DtoCustomerUsageSummaryResponse;
    /** Customer entitlements */
    entitlements?: DtoCustomerEntitlementsResponse;
    /** Customer wallet balance */
    walletBalance?: DtoWalletBalanceResponse;
    /** Active subscriptions */
    activeSubscriptions?: DtoSubscriptionResponse[];
    /** Recent invoices */
    invoices?: DtoInvoiceResponse[];
    /** Invoice summary */
    summary?: DtoCustomerMultiCurrencyInvoiceSummary;
    /** Usage analytics */
    analytics?: DtoGetUsageAnalyticsResponse;
    /** Available features */
    features?: DtoFeatureResponse[];
    /** Metadata */
    metadata: {
        fetchedAt: string;
        customerId: string;
        totalSubscriptions?: number;
        totalInvoices?: number;
        totalWallets?: number;
        totalFeatures?: number;
        errors?: string[];
        warnings?: string[];
    };
}

/**
 * Detailed subscription information
 */
export interface SubscriptionDetails {
    subscription: DtoSubscriptionResponse;
    invoices: DtoInvoiceResponse[];
    entitlements: DtoEntitlementResponse[];
    usage?: DtoCustomerUsageSummaryResponse;
}

/**
 * Customer portal summary
 */
export interface CustomerPortalSummary {
    customer: DtoCustomerResponse;
    activeSubscriptions: number;
    totalInvoices: number;
    totalSpent: number;
    currency: string;
    nextBillingDate?: string;
    status: TypesSubscriptionStatus;
}

/**
 * Individual method response types
 */
export interface UsageResponse {
    data?: DtoCustomerUsageSummaryResponse;
    error?: string;
}

export interface EntitlementsResponse {
    data?: DtoCustomerEntitlementsResponse;
    error?: string;
}

export interface WalletBalanceResponse {
    data?: DtoWalletBalanceResponse;
    error?: string;
}

export interface ActiveSubscriptionsResponse {
    data?: DtoSubscriptionResponse[];
    error?: string;
}

export interface RecentInvoicesResponse {
    data?: DtoInvoiceResponse[];
    error?: string;
}

/**
 * Customer Portal class for dashboard data
 */
export class CustomerPortal {
    private customersApi: CustomersApi;
    private subscriptionsApi: SubscriptionsApi;
    private invoicesApi: InvoicesApi;
    private walletsApi: WalletsApi;
    private entitlementsApi: EntitlementsApi;
    private featuresApi: FeaturesApi;

    constructor(configuration?: runtime.Configuration) {
        // Initialize API instances with the same configuration as other SDK APIs
        this.customersApi = new CustomersApi(configuration);
        this.subscriptionsApi = new SubscriptionsApi(configuration);
        this.invoicesApi = new InvoicesApi(configuration);
        this.walletsApi = new WalletsApi(configuration);
        this.entitlementsApi = new EntitlementsApi(configuration);
        this.featuresApi = new FeaturesApi(configuration);
    }

    /**
     * Get complete dashboard data - the main method you'll use
     * 
     * @param customerExternalId - Customer external ID
     * @param options - Dashboard options
     * @returns Complete customer dashboard data
     * 
     * @example
     * ```typescript
     * const configuration = new Configuration({ apiKey: 'your-key' });
     * const customerPortal = new CustomerPortal(configuration);
     * const data = await customerPortal.getDashboardData('customer-123', {
     *   subscriptionLimit: 10,
     *   invoiceLimit: 5,
     *   days: 30
     * });
     * ```
     */
    async getDashboardData(customerExternalId: string, options: DashboardOptions = {}): Promise<CustomerDashboardData> {
        const opts = {
            subscriptionLimit: 10,
            invoiceLimit: 5,
            subscriptionStatus: [SubscriptionsGetSubscriptionStatusEnum.ACTIVE],
            invoiceStatus: [InvoicesGetInvoiceStatusEnum.FINALIZED],
            includeCustomer: true,
            includeSubscriptions: true,
            includeInvoices: true,
            includeUsage: true,
            includeEntitlements: true,
            includeSummary: true,
            includeAnalytics: false,
            includeFeatures: false,
            includeWalletBalance: true,
            entitlementLimit: 50,
            ...options
        };

        const errors: string[] = [];
        const warnings: string[] = [];
        const now = new Date().toISOString();

        // Helper to safely call APIs with type safety
        const safeCall = async <T>(operation: ApiOperationType, call: () => Promise<T>): Promise<T | undefined> => {
            try {
                return await call();
            } catch (error) {
                const errorMsg = `${operation}: ${error instanceof Error ? error.message : String(error)}`;
                errors.push(errorMsg);
                console.warn(`CustomerPortal API Warning: ${errorMsg}`);
                return undefined;
            }
        };

        // Get customer by external ID to get the actual customer ID
        const customer = await safeCall(ApiOperationType.CUSTOMER_LOOKUP, () =>
            this.customersApi.customersLookupLookupKeyGet({
                lookupKey: customerExternalId
            })
        );

        if (!customer?.id) {
            return {
                metadata: {
                    fetchedAt: now,
                    customerId: customerExternalId,
                    errors: [`Customer not found for external ID: ${customerExternalId}`]
                }
            };
        }

        const customerId = customer.id;

        // Calculate time range
        let startTime: string | undefined;
        let endTime: string | undefined;

        if (opts.days) {
            const start = new Date();
            start.setDate(start.getDate() - opts.days);
            startTime = start.toISOString();
            endTime = now;
        } else if (opts.startDate && opts.endDate) {
            startTime = opts.startDate;
            endTime = opts.endDate;
        }

        // Fetch all data in parallel
        const [
            usage,
            entitlements,
            walletBalance,
            subscriptions,
            invoices,
            summary,
            analytics,
            features
        ] = await Promise.all([

            // Usage summary
            opts.includeUsage
                ? safeCall(ApiOperationType.USAGE, () => this.customersApi.customersIdUsageGet({
                    id: customerId,
                    featureIds: opts.featureIds,
                    subscriptionIds: opts.subscriptionIds
                }))
                : Promise.resolve(undefined),

            // Entitlements
            opts.includeEntitlements
                ? safeCall(ApiOperationType.ENTITLEMENTS, () => this.customersApi.customersIdEntitlementsGet({
                    id: customerId,
                    featureIds: opts.featureIds,
                    subscriptionIds: opts.subscriptionIds
                }))
                : Promise.resolve(undefined),

            // Wallet balance
            opts.includeWalletBalance
                ? safeCall(ApiOperationType.WALLET, () => this.getWalletBalance(customerId))
                : Promise.resolve(undefined),

            // Subscriptions
            opts.includeSubscriptions
                ? safeCall(ApiOperationType.SUBSCRIPTIONS, () => this.subscriptionsApi.subscriptionsGet({
                    customerId: customerId,
                    limit: opts.subscriptionLimit,
                    subscriptionStatus: opts.subscriptionStatus,
                    startTime,
                    endTime
                }))
                : Promise.resolve(undefined),

            // Invoices
            opts.includeInvoices
                ? safeCall(ApiOperationType.INVOICES, () => this.invoicesApi.invoicesGet({
                    customerId: customerId,
                    limit: opts.invoiceLimit,
                    invoiceStatus: opts.invoiceStatus,
                    startTime,
                    endTime
                }))
                : Promise.resolve(undefined),

            // Invoice summary
            opts.includeSummary
                ? safeCall(ApiOperationType.SUMMARY, () => this.invoicesApi.customersIdInvoicesSummaryGet({ id: customerId }))
                : Promise.resolve(undefined),

            // Analytics (placeholder - implement when available)
            opts.includeAnalytics
                ? Promise.resolve(undefined)
                : Promise.resolve(undefined),

            // Features
            opts.includeFeatures
                ? safeCall(ApiOperationType.FEATURES, () => this.featuresApi.featuresGet())
                : Promise.resolve(undefined),
        ]);

        // Extract subscription and invoice items
        const activeSubscriptions = (subscriptions as DtoListSubscriptionsResponse)?.items || [];
        const recentInvoices = (invoices as DtoListInvoicesResponse)?.items || [];

        return {
            customer,
            usage,
            entitlements,
            walletBalance: walletBalance?.data,
            activeSubscriptions,
            invoices: recentInvoices,
            summary,
            analytics,
            features: (features as any)?.data || undefined,
            metadata: {
                fetchedAt: now,
                customerId: customerExternalId,
                totalSubscriptions: activeSubscriptions.length,
                totalInvoices: recentInvoices.length,
                totalWallets: walletBalance?.data ? 1 : 0,
                totalFeatures: (features as any)?.data?.length || 0,
                errors: errors.length > 0 ? errors : undefined,
                warnings: warnings.length > 0 ? warnings : undefined,
            }
        };
    }

    /**
     * Get customer data by external ID
     * 
     * @param externalId - External customer ID
     * @param options - Dashboard options
     * @returns Customer data
     */
    async getCustomerDataByExternalId(
        externalId: string,
        options: DashboardOptions = {}
    ): Promise<CustomerDashboardData> {
        try {
            const customer = await this.customersApi.customersLookupLookupKeyGet({
                lookupKey: externalId
            });

            if (!customer.id) {
                return {
                    metadata: {
                        fetchedAt: new Date().toISOString(),
                        customerId: externalId,
                        errors: [`Customer not found for external ID: ${externalId}`]
                    }
                };
            }

            return await this.getDashboardData(externalId, options);
        } catch (error) {
            return {
                metadata: {
                    fetchedAt: new Date().toISOString(),
                    customerId: externalId,
                    errors: [`External ID lookup failed: ${error instanceof Error ? error.message : String(error)}`]
                }
            };
        }
    }

    /**
     * Get detailed subscription information
     * 
     * @param subscriptionId - Subscription ID
     * @param options - Options for related data
     * @returns Detailed subscription data
     */
    async getSubscriptionDetails(
        subscriptionId: string,
        options: DashboardOptions = {}
    ): Promise<SubscriptionDetails | null> {
        try {
            const subscription = await this.subscriptionsApi.subscriptionsIdGet({ id: subscriptionId });

            if (!subscription) {
                return null;
            }

            const customerId = subscription.customerId;
            if (!customerId) {
                return null;
            }

            // Fetch related data in parallel
            const [invoices, entitlements, usage] = await Promise.all([
                this.invoicesApi.invoicesGet({
                    customerId,
                    subscriptionId,
                    limit: options.invoiceLimit || 10,
                    invoiceStatus: options.invoiceStatus || [InvoicesGetInvoiceStatusEnum.FINALIZED]
                }).catch(() => ({ data: [] })),

                this.entitlementsApi.entitlementsGet().catch(() => ({ data: [] })),

                this.customersApi.customersIdUsageGet({ id: customerId }).catch(() => undefined)
            ]);

            return {
                subscription,
                invoices: (invoices as any).data || [],
                entitlements: (entitlements as any).data || [],
                usage
            };
        } catch (error) {
            console.error('Error fetching subscription details:', error);
            return null;
        }
    }

    /**
     * Get customer portal summary
     * 
     * @param customerExternalId - Customer external ID
     * @returns Customer summary
     */
    async getCustomerSummary(customerExternalId: string): Promise<CustomerPortalSummary | null> {
        try {
            const data = await this.getDashboardData(customerExternalId, {
                includeCustomer: true,
                includeSubscriptions: true,
                includeInvoices: true,
                subscriptionLimit: 100,
                invoiceLimit: 100
            });

            if (!data.customer) {
                return null;
            }

            const activeSubscriptions = data.activeSubscriptions?.filter(
                (sub: any) => sub.status === SubscriptionsGetSubscriptionStatusEnum.ACTIVE
            ).length || 0;

            const totalInvoices = data.invoices?.length || 0;

            // Calculate total spent from invoices
            let totalSpent = 0;
            let currency = 'USD';

            if (data.invoices) {
                for (const invoice of data.invoices) {
                    if ((invoice as any).amount && (invoice as any).currency) {
                        totalSpent += (invoice as any).amount;
                        currency = (invoice as any).currency;
                    }
                }
            }

            // Get next billing date from active subscriptions
            let nextBillingDate: string | undefined;
            if (data.activeSubscriptions) {
                const activeSubs = data.activeSubscriptions.filter((sub: any) => sub.status === SubscriptionsGetSubscriptionStatusEnum.ACTIVE);
                if (activeSubs.length > 0) {
                    const nextBilling = activeSubs
                        .map((sub: any) => sub.nextBillingDate)
                        .filter((date: any) => date)
                        .sort()
                    [0];
                    nextBillingDate = nextBilling;
                }
            }

            // Determine overall status
            let status: TypesSubscriptionStatus = TypesSubscriptionStatus.SubscriptionStatusCancelled;
            if (activeSubscriptions > 0) {
                status = TypesSubscriptionStatus.SubscriptionStatusActive;
            } else if (data.activeSubscriptions?.some((sub: any) => sub.status === TypesSubscriptionStatus.SubscriptionStatusPaused)) {
                status = TypesSubscriptionStatus.SubscriptionStatusPaused;
            } else if (data.activeSubscriptions?.some((sub: any) => sub.status === TypesSubscriptionStatus.SubscriptionStatusPastDue)) {
                status = TypesSubscriptionStatus.SubscriptionStatusPastDue;
            } else if (data.activeSubscriptions?.some((sub: any) => sub.status === TypesSubscriptionStatus.SubscriptionStatusUnpaid)) {
                status = TypesSubscriptionStatus.SubscriptionStatusUnpaid;
            } else if (data.activeSubscriptions?.some((sub: any) => sub.status === TypesSubscriptionStatus.SubscriptionStatusTrialing)) {
                status = TypesSubscriptionStatus.SubscriptionStatusTrialing;
            }

            return {
                customer: data.customer,
                activeSubscriptions,
                totalInvoices,
                totalSpent,
                currency,
                nextBillingDate,
                status
            };
        } catch (error) {
            console.error('Error fetching customer summary:', error);
            return null;
        }
    }

    /**
     * Search customers by email or external ID
     * 
     * @param query - Search query
     * @param options - Search options
     * @returns Search results
     */
    async searchCustomers(
        query: string,
        options: { limit?: number } = {}
    ): Promise<DtoCustomerResponse[]> {
        try {
            // Try exact lookup by external ID first
            const match = await this.customersApi.customersLookupLookupKeyGet({ 
                lookupKey: query 
            }).catch(() => undefined);
            
            if (match?.id) {
                return [match];
            }
            
            // Fallback to list with server-side filtering
            const result = await this.customersApi.customersGet({
                limit: options.limit || 10,
                // Try both email and externalId filters
                email: query.includes('@') ? query : undefined,
                externalId: !query.includes('@') ? query : undefined
            });
            
            return (result as any).data || [];
        } catch (error) {
            console.error('Error searching customers:', error);
            return [];
        }
    }

    /**
     * Get customer usage analytics
     * 
     * @param customerId - Customer ID
     * @param options - Analytics options
     * @returns Usage analytics
     */
    async getUsageAnalytics(
        customerId: string,
        options: {
            startDate?: string;
            endDate?: string;
            meterIds?: string[];
        } = {}
    ): Promise<DtoGetUsageAnalyticsResponse | null> {
        try {
            const result = await this.customersApi.customersIdUsageGet({
                id: customerId
            });
            return result as any;
        } catch (error) {
            console.error('Error fetching usage analytics:', error);
            return null;
        }
    }


    /**
     * Get customer wallets with real-time balance
     * 
     * @param customerId - Customer ID
     * @param includeRealTimeBalance - Include real-time balance
     * @returns Customer wallets
     */
    async getCustomerWallets(
        customerId: string,
        includeRealTimeBalance: boolean = true
    ): Promise<any[]> {
        try {
            const result = await this.walletsApi.customersWalletsGet({
                id: customerId,
                includeRealTimeBalance
            });
            return (result as any)?.data || [];
        } catch (error) {
            console.error('Error fetching customer wallets:', error);
            return [];
        }
    }

    /**
     * Get wallet transactions
     * 
     * @param walletId - Wallet ID
     * @param options - Transaction options
     * @returns Wallet transactions
     */
    async getWalletTransactions(
        walletId: string,
        options: {
            limit?: number;
            offset?: number;
            status?: string[];
        } = {}
    ): Promise<any[]> {
        try {
            const result = await this.walletsApi.walletsIdTransactionsGet({
                id: walletId,
                limit: options.limit || 50,
                offset: options.offset || 0,
            });
            return (result as any)?.data || [];
        } catch (error) {
            console.error('Error fetching wallet transactions:', error);
            return [];
        }
    }

    // Individual method implementations for granular access

    /**
     * Get customer usage data
     */
    async getUsage(customerId: string): Promise<UsageResponse> {
        try {
            const response = await this.customersApi.customersIdUsageGet({
                id: customerId
            });
            return { data: response as DtoCustomerUsageSummaryResponse };
        } catch (error) {
            return {
                error: error instanceof Error ? error.message : String(error)
            };
        }
    }

    /**
     * Get customer entitlements
     */
    async getEntitlements(customerId: string): Promise<EntitlementsResponse> {
        try {
            const response = await this.customersApi.customersIdEntitlementsGet({
                id: customerId
            });
            return { data: response as DtoCustomerEntitlementsResponse };
        } catch (error) {
            return {
                error: error instanceof Error ? error.message : String(error)
            };
        }
    }

    /**
     * Get customer wallet balance
     */
    async getWalletBalance(customerId: string): Promise<WalletBalanceResponse> {
        try {
            // First, get customer's wallets
            const walletsResponse = await this.walletsApi.customersWalletsGet({
                id: customerId,
                includeRealTimeBalance: true
            });

            const wallets = (walletsResponse as any)?.data || [];
            if (wallets.length === 0) {
                return { error: 'No wallet found for customer' };
            }

            // Get the first wallet's balance
            const wallet = wallets[0];
            const balanceResponse = await this.walletsApi.walletsIdBalanceRealTimeGet({
                id: wallet.id
            });

            return { data: balanceResponse as DtoWalletBalanceResponse };
        } catch (error) {
            return {
                error: error instanceof Error ? error.message : String(error)
            };
        }
    }

    /**
     * Get active subscriptions
     */
    async getActiveSubscriptions(
        customerId: string,
        limit = 10,
        status?: SubscriptionsGetSubscriptionStatusEnum[],
        startTime?: string,
        endTime?: string
    ): Promise<ActiveSubscriptionsResponse> {
        try {
            const response = await this.subscriptionsApi.subscriptionsGet({
                customerId: customerId,
                limit,
                subscriptionStatus: status,
                startTime,
                endTime
            });

            const subscriptions = (response as DtoListSubscriptionsResponse)?.items || [];
            return { data: subscriptions };
        } catch (error) {
            return {
                error: error instanceof Error ? error.message : String(error)
            };
        }
    }

    /**
     * Get recent invoices
     */
    async getRecentInvoices(
        customerId: string,
        limit = 5,
        status?: InvoicesGetInvoiceStatusEnum[],
        startTime?: string,
        endTime?: string
    ): Promise<RecentInvoicesResponse> {
        try {
            const response = await this.invoicesApi.invoicesGet({
                customerId: customerId,
                limit,
                invoiceStatus: status,
                startTime,
                endTime
            });

            const invoices = (response as DtoListInvoicesResponse)?.items || [];
            return { data: invoices };
        } catch (error) {
            return {
                error: error instanceof Error ? error.message : String(error)
            };
        }
    }

    /**
     * Get invoice PDF
     * 
     * @param invoiceId - Invoice ID
     * @returns Invoice PDF URL or data
     */
    async getInvoicePDF(invoiceId: string): Promise<string | null> {
        try {
            const response = await this.invoicesApi.invoicesIdPdfGet({
                id: invoiceId
            });
            return (response as any)?.url || null;
        } catch (error) {
            console.error('Error fetching invoice PDF:', error);
            return null;
        }
    }

}

/**
 * Factory function to create CustomerPortal instance
 */
export function createCustomerPortal(
    configuration?: runtime.Configuration
): CustomerPortal {
    return new CustomerPortal(configuration);
}

/**
 * Quick one-liner function for dashboard data
 */
export async function getCustomerDashboardData(
    customerExternalId: string,
    options?: DashboardOptions,
    configuration?: runtime.Configuration
): Promise<CustomerDashboardData> {
    const portal = new CustomerPortal(configuration);
    return portal.getDashboardData(customerExternalId, options);
}

/**
 * One-liner function for customer summary
 */
export async function getCustomerSummary(
    customerExternalId: string,
    configuration?: runtime.Configuration
): Promise<CustomerPortalSummary | null> {
    const portal = new CustomerPortal(configuration);
    return portal.getCustomerSummary(customerExternalId);
}

/**
 * One-liner function for subscription details
 */
export async function getSubscriptionDetails(
    subscriptionId: string,
    options?: DashboardOptions,
    configuration?: runtime.Configuration
): Promise<SubscriptionDetails | null> {
    const portal = new CustomerPortal(configuration);
    return portal.getSubscriptionDetails(subscriptionId, options);
}

/**
 * One-liner function for customer data by external ID
 */
export async function getCustomerDataByExternalId(
    externalId: string,
    options?: DashboardOptions,
    configuration?: runtime.Configuration
): Promise<CustomerDashboardData> {
    const portal = new CustomerPortal(configuration);
    return portal.getCustomerDataByExternalId(externalId, options);
}