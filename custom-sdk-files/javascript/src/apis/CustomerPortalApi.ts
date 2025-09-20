/* tslint:disable */
/* eslint-disable */
/**
 * FlexPrice Customer Portal API - Simple & Easy to Use
 * 
 * Minimal, intuitive API for fetching customer portal data
 * with maximum ease of integration for frontend developers.
 */

import * as runtime from '../runtime';
import type {
    DtoCustomerResponse,
    DtoCustomerUsageSummaryResponse,
    DtoCustomerEntitlementsResponse,
    DtoListSubscriptionsResponse,
    DtoListInvoicesResponse,
    DtoListPaymentsResponse,
    DtoCustomerMultiCurrencyInvoiceSummary,
    DtoGetUsageAnalyticsResponse,
    DtoSubscriptionResponse,
    DtoInvoiceResponse,
    DtoPaymentResponse,
    DtoEntitlementResponse,
    DtoFeatureResponse,
} from '../models/index';
import {
    SubscriptionsGetSubscriptionStatusEnum,
    InvoicesGetInvoiceStatusEnum,
} from './index';
import { CustomersApi } from './CustomersApi';
import { SubscriptionsApi } from './SubscriptionsApi';
import { InvoicesApi } from './InvoicesApi';
import { PaymentsApi } from './PaymentsApi';
import { EntitlementsApi } from './EntitlementsApi';
import { FeaturesApi } from './FeaturesApi';

/**
 * Simple configuration options
 */
export interface CustomerPortalOptions {
    // What to include
    includeCustomer?: boolean;
    includeSubscriptions?: boolean;
    includeInvoices?: boolean;
    includePayments?: boolean;
    includeUsage?: boolean;
    includeEntitlements?: boolean;
    includeSummary?: boolean;
    includeAnalytics?: boolean;
    includeFeatures?: boolean;

    // Limits
    subscriptionLimit?: number;
    invoiceLimit?: number;
    paymentLimit?: number;
    entitlementLimit?: number;

    // Time range
    days?: number; // Last N days
    startDate?: string;
    endDate?: string;

    // Filters
    subscriptionStatus?: SubscriptionsGetSubscriptionStatusEnum[];
    invoiceStatus?: InvoicesGetInvoiceStatusEnum[];
    paymentStatus?: string[];
}

/**
 * Simple response structure
 */
export interface CustomerPortalData {
    customer?: DtoCustomerResponse;
    subscriptions?: DtoListSubscriptionsResponse;
    invoices?: DtoListInvoicesResponse;
    payments?: DtoListPaymentsResponse;
    usage?: DtoCustomerUsageSummaryResponse;
    entitlements?: DtoCustomerEntitlementsResponse;
    summary?: DtoCustomerMultiCurrencyInvoiceSummary;
    analytics?: DtoGetUsageAnalyticsResponse;
    features?: DtoFeatureResponse[];

    // Simple metadata
    fetchedAt: string;
    errors?: string[];
    warnings?: string[];
}

/**
 * Detailed subscription information
 */
export interface SubscriptionDetails {
    subscription: DtoSubscriptionResponse;
    invoices: DtoInvoiceResponse[];
    payments: DtoPaymentResponse[];
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
    totalPayments: number;
    totalSpent: number;
    currency: string;
    lastPaymentDate?: string;
    nextBillingDate?: string;
    status: 'active' | 'inactive' | 'suspended' | 'cancelled';
}

/**
 * Super simple Customer Portal API
 */
export class CustomerPortalApi extends runtime.BaseAPI {

    /**
     * Get customer portal data - the only method you need!
     * 
     * @param customerId - Customer ID or external ID
     * @param options - Simple options (all optional)
     * @returns All customer data in one call
     * 
     * @example
     * ```typescript
     * // Simplest usage - gets everything
     * const data = await api.getCustomerData('customer-123');
     * 
     * // Just what you need
     * const data = await api.getCustomerData('customer-123', {
     *   includeCustomer: true,
     *   includeSubscriptions: true,
     *   subscriptionLimit: 5,
     *   days: 30
     * });
     * ```
     */
    async getCustomerData(
        customerId: string,
        options: CustomerPortalOptions = {}
    ): Promise<CustomerPortalData> {

        // Set defaults - everything included by default
        const opts = {
            includeCustomer: true,
            includeSubscriptions: true,
            includeInvoices: true,
            includePayments: true,
            includeUsage: true,
            includeEntitlements: true,
            includeSummary: true,
            includeAnalytics: false,
            includeFeatures: false,
            subscriptionLimit: 10,
            invoiceLimit: 10,
            paymentLimit: 10,
            entitlementLimit: 50,
            subscriptionStatus: [SubscriptionsGetSubscriptionStatusEnum.ACTIVE],
            invoiceStatus: [InvoicesGetInvoiceStatusEnum.FINALIZED],
            paymentStatus: ['SUCCEEDED'],
            ...options
        };

        const errors: string[] = [];
        const warnings: string[] = [];
        const now = new Date().toISOString();

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

        // Helper to safely call APIs
        const safeCall = async <T>(name: string, call: () => Promise<T>): Promise<T | undefined> => {
            try {
                return await call();
            } catch (error) {
                const errorMsg = `${name}: ${error instanceof Error ? error.message : String(error)}`;
                errors.push(errorMsg);
                console.warn(`CustomerPortal API Warning: ${errorMsg}`);
                return undefined;
            }
        };

        // Create API instances
        const customersApi = new CustomersApi();
        const subscriptionsApi = new SubscriptionsApi();
        const invoicesApi = new InvoicesApi();
        const paymentsApi = new PaymentsApi();
        const entitlementsApi = new EntitlementsApi();
        const featuresApi = new FeaturesApi();

        // Fetch all data in parallel
        const [
            customer,
            subscriptions,
            invoices,
            payments,
            usage,
            entitlements,
            summary,
            analytics,
            features
        ] = await Promise.all([
            // Customer details
            opts.includeCustomer
                ? safeCall('Customer', () => customersApi.customersIdGet({ id: customerId }))
                : Promise.resolve(undefined),

            // Subscriptions
            opts.includeSubscriptions
                ? safeCall('Subscriptions', () => subscriptionsApi.subscriptionsGet({
                    customerId,
                    limit: opts.subscriptionLimit,
                    subscriptionStatus: opts.subscriptionStatus,
                    startTime,
                    endTime
                }))
                : Promise.resolve(undefined),

            // Invoices
            opts.includeInvoices
                ? safeCall('Invoices', () => invoicesApi.invoicesGet({
                    customerId,
                    limit: opts.invoiceLimit,
                    invoiceStatus: opts.invoiceStatus,
                    startTime,
                    endTime
                }))
                : Promise.resolve(undefined),

            // Payments
            opts.includePayments
                ? safeCall('Payments', () => paymentsApi.paymentsGet({
                    destinationId: customerId,
                    destinationType: 'customer',
                    limit: opts.paymentLimit,
                    paymentStatus: opts.paymentStatus?.[0] || 'SUCCEEDED',
                    startTime,
                    endTime
                }))
                : Promise.resolve(undefined),

            // Usage summary
            opts.includeUsage
                ? safeCall('Usage', () => customersApi.customersIdUsageGet({ id: customerId }))
                : Promise.resolve(undefined),

            // Entitlements
            opts.includeEntitlements
                ? safeCall('Entitlements', () => customersApi.customersIdEntitlementsGet({
                    id: customerId
                }))
                : Promise.resolve(undefined),

            // Invoice summary
            opts.includeSummary
                ? safeCall('Summary', () => invoicesApi.customersIdInvoicesSummaryGet({ id: customerId }))
                : Promise.resolve(undefined),

            // Analytics (placeholder - implement when available)
            opts.includeAnalytics
                ? Promise.resolve(undefined)
                : Promise.resolve(undefined),

            // Features
            opts.includeFeatures
                ? safeCall('Features', () => featuresApi.featuresGet())
                : Promise.resolve(undefined),
        ]);

        return {
            customer,
            subscriptions,
            invoices,
            payments,
            usage,
            entitlements,
            summary,
            analytics,
            features: (features as any)?.data || undefined,
            fetchedAt: now,
            errors: errors.length > 0 ? errors : undefined,
            warnings: warnings.length > 0 ? warnings : undefined
        };
    }

    /**
     * Get customer data by external ID
     * 
     * @param externalId - External customer ID
     * @param options - Simple options
     * @returns Customer data
     */
    async getCustomerDataByExternalId(
        externalId: string,
        options: CustomerPortalOptions = {}
    ): Promise<CustomerPortalData> {
        const customersApi = new CustomersApi();

        try {
            const customer = await customersApi.customersLookupLookupKeyGet({
                lookupKey: externalId
            });

            if (!customer.id) {
                return {
                    fetchedAt: new Date().toISOString(),
                    errors: [`Customer not found for external ID: ${externalId}`]
                };
            }

            return await this.getCustomerData(customer.id, options);
        } catch (error) {
            return {
                fetchedAt: new Date().toISOString(),
                errors: [`External ID lookup failed: ${error instanceof Error ? error.message : String(error)}`]
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
        options: CustomerPortalOptions = {}
    ): Promise<SubscriptionDetails | null> {
        const subscriptionsApi = new SubscriptionsApi();
        const invoicesApi = new InvoicesApi();
        const paymentsApi = new PaymentsApi();
        const entitlementsApi = new EntitlementsApi();
        const customersApi = new CustomersApi();

        try {
            const subscription = await subscriptionsApi.subscriptionsIdGet({ id: subscriptionId });

            if (!subscription) {
                return null;
            }

            const customerId = subscription.customerId;
            if (!customerId) {
                return null;
            }

            // Fetch related data in parallel
            const [invoices, payments, entitlements, usage] = await Promise.all([
                invoicesApi.invoicesGet({
                    customerId,
                    subscriptionId,
                    limit: options.invoiceLimit || 10,
                    invoiceStatus: options.invoiceStatus || [InvoicesGetInvoiceStatusEnum.FINALIZED]
                }).catch(() => ({ data: [] })),

                paymentsApi.paymentsGet({
                    destinationId: subscriptionId,
                    destinationType: 'subscription',
                    limit: options.paymentLimit || 10,
                    paymentStatus: options.paymentStatus?.[0] || 'SUCCEEDED'
                }).catch(() => ({ data: [] })),

                entitlementsApi.entitlementsGet().catch(() => ({ data: [] })),

                customersApi.customersIdUsageGet({ id: customerId }).catch(() => undefined)
            ]);

            return {
                subscription,
                invoices: (invoices as any).data || [],
                payments: (payments as any).data || [],
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
     * @param customerId - Customer ID
     * @returns Customer summary
     */
    async getCustomerSummary(customerId: string): Promise<CustomerPortalSummary | null> {
        try {
            const data = await this.getCustomerData(customerId, {
                includeCustomer: true,
                includeSubscriptions: true,
                includeInvoices: true,
                includePayments: true,
                subscriptionLimit: 100,
                invoiceLimit: 100,
                paymentLimit: 100
            });

            if (!data.customer) {
                return null;
            }

            const activeSubscriptions = (data.subscriptions as any)?.data?.filter(
                (sub: any) => sub.status === 'ACTIVE'
            ).length || 0;

            const totalInvoices = (data.invoices as any)?.data?.length || 0;
            const totalPayments = (data.payments as any)?.data?.length || 0;

            // Calculate total spent
            let totalSpent = 0;
            let currency = 'USD';

            if ((data.payments as any)?.data) {
                for (const payment of (data.payments as any).data) {
                    if (payment.amount && payment.currency) {
                        totalSpent += payment.amount;
                        currency = payment.currency;
                    }
                }
            }

            // Get last payment date
            const lastPayment = (data.payments as any)?.data
                ?.sort((a: any, b: any) => new Date(b.createdAt || 0).getTime() - new Date(a.createdAt || 0).getTime())
                ?.[0];

            // Get next billing date from active subscriptions
            let nextBillingDate: string | undefined;
            if ((data.subscriptions as any)?.data) {
                const activeSubs = (data.subscriptions as any).data.filter((sub: any) => sub.status === 'ACTIVE');
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
            let status: 'active' | 'inactive' | 'suspended' | 'cancelled' = 'inactive';
            if (activeSubscriptions > 0) {
                status = 'active';
            } else if ((data.subscriptions as any)?.data?.some((sub: any) => sub.status === 'SUSPENDED')) {
                status = 'suspended';
            } else if ((data.subscriptions as any)?.data?.some((sub: any) => sub.status === 'CANCELLED')) {
                status = 'cancelled';
            }

            return {
                customer: data.customer,
                activeSubscriptions,
                totalInvoices,
                totalPayments,
                totalSpent,
                currency,
                lastPaymentDate: lastPayment?.createdAt,
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
        const customersApi = new CustomersApi();

        try {
            const result = await customersApi.customersGet({
                limit: options.limit || 10
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
            const customersApi = new CustomersApi();

            const result = await customersApi.customersIdUsageGet({
                id: customerId
            });

            return result as any;
        } catch (error) {
            console.error('Error fetching usage analytics:', error);
            return null;
        }
    }
}

/**
 * Quick factory function
 */
export function createCustomerPortalApi(configuration?: runtime.Configuration): CustomerPortalApi {
    return new CustomerPortalApi(configuration);
}

/**
 * One-liner function for quick data fetching
 */
export async function getCustomerData(
    customerId: string,
    options?: CustomerPortalOptions,
    configuration?: runtime.Configuration
): Promise<CustomerPortalData> {
    const api = new CustomerPortalApi(configuration);
    return api.getCustomerData(customerId, options);
}

/**
 * One-liner function for customer summary
 */
export async function getCustomerSummary(
    customerId: string,
    configuration?: runtime.Configuration
): Promise<CustomerPortalSummary | null> {
    const api = new CustomerPortalApi(configuration);
    return api.getCustomerSummary(customerId);
}

/**
 * One-liner function for subscription details
 */
export async function getSubscriptionDetails(
    subscriptionId: string,
    options?: CustomerPortalOptions,
    configuration?: runtime.Configuration
): Promise<SubscriptionDetails | null> {
    const api = new CustomerPortalApi(configuration);
    return api.getSubscriptionDetails(subscriptionId, options);
}
