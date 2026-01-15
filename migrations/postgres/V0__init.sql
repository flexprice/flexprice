--
-- PostgreSQL database dump
--

-- Dumped from database version 15.6
-- Dumped by pg_dump version 17.2

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

--
-- Name: public; Type: SCHEMA; Schema: -; Owner: -
--

CREATE SCHEMA IF NOT EXISTS public;

-- Create extensions schema and install uuid extension
CREATE SCHEMA IF NOT EXISTS extensions;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp" SCHEMA extensions;

--
-- Name: SCHEMA public; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON SCHEMA public IS 'standard public schema';


SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: auths; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auths (
    user_id character varying(50) NOT NULL,
    provider character varying(20) NOT NULL,
    token text NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: customers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.customers (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    external_id character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    email character varying(255),
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    address_line1 character varying(255),
    address_line2 character varying(255),
    address_city character varying(100),
    address_state character varying(100),
    address_postal_code character varying(20),
    address_country character varying(2),
    parent_customer_id character varying(50),
    metadata jsonb
);


--
-- Name: environments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.environments (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    name character varying(50) NOT NULL,
    type character varying(20) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying
);


--
-- Name: invoice_line_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoice_line_items (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    invoice_id character varying(50) NOT NULL,
    customer_id character varying(50) NOT NULL,
    subscription_id character varying(50),
    entity_id character varying(50),
    entity_type character varying(50),
    plan_display_name character varying,
    price_id character varying(50),
    price_type character varying(50),
    meter_id character varying(50),
    meter_display_name character varying,
    price_unit_id character varying(50),
    price_unit character varying(3),
    price_unit_amount numeric(20,8),
    display_name character varying,
    amount numeric(20,8) DEFAULT 0 NOT NULL,
    quantity numeric(20,8) DEFAULT 0 NOT NULL,
    currency character varying(10) NOT NULL,
    period_start timestamp with time zone,
    period_end timestamp with time zone,
    metadata jsonb,
    commitment_info jsonb
);

--
-- Name: invoices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoices (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    customer_id character varying(50) NOT NULL,
    subscription_id character varying(50),
    invoice_type character varying(50) NOT NULL,
    invoice_status character varying(50) DEFAULT 'DRAFT'::character varying NOT NULL,
    payment_status character varying(50) DEFAULT 'PENDING'::character varying NOT NULL,
    currency character varying(10) NOT NULL,
    amount_due numeric(20,8) DEFAULT 0 NOT NULL,
    amount_paid numeric(20,8) DEFAULT 0 NOT NULL,
    amount_remaining numeric(20,8) DEFAULT 0 NOT NULL,
    subtotal numeric(20,8) DEFAULT 0,
    adjustment_amount numeric(20,8) DEFAULT 0,
    refunded_amount numeric(20,8) DEFAULT 0,
    total_tax numeric(20,8) DEFAULT 0,
    total_discount numeric(20,8) DEFAULT 0,
    total numeric(20,8) DEFAULT 0,
    description character varying,
    due_date timestamp with time zone,
    paid_at timestamp with time zone,
    voided_at timestamp with time zone,
    finalized_at timestamp with time zone,
    billing_period character varying,
    period_start timestamp with time zone,
    period_end timestamp with time zone,
    invoice_pdf_url character varying,
    billing_reason character varying,
    metadata jsonb,
    version bigint DEFAULT 1 NOT NULL,
    invoice_number character varying(50),
    billing_sequence integer,
    idempotency_key character varying(100)
);


--
-- Name: meters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.meters (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    event_name character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    aggregation jsonb DEFAULT '{"type":"count","field":""}'::jsonb NOT NULL,
    filters jsonb DEFAULT '[]'::jsonb NOT NULL,
    reset_usage character varying(20) DEFAULT 'BILLING_PERIOD'::character varying NOT NULL
);


--
-- Name: plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plans (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    lookup_key character varying(255),
    name character varying(255) NOT NULL,
    description text,
    display_order integer DEFAULT 0 NOT NULL,
    metadata jsonb
);


--
-- Name: prices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prices (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    display_name character varying(255),
    amount numeric(25,15) DEFAULT 0 NOT NULL,
    currency character varying(3) NOT NULL,
    display_amount character varying(255),
    price_unit_type character varying(20) DEFAULT 'FIAT'::character varying NOT NULL,
    price_unit_id character varying(50),
    price_unit character varying(3),
    price_unit_amount numeric(25,15) DEFAULT 0,
    display_price_unit_amount character varying(255),
    conversion_rate numeric(25,15) DEFAULT 1,
    min_quantity numeric(20,8),
    type character varying(20) NOT NULL,
    billing_period character varying(20) NOT NULL,
    billing_period_count integer NOT NULL,
    billing_model character varying(20) NOT NULL,
    billing_cadence character varying(20) NOT NULL,
    invoice_cadence character varying(20),
    trial_period integer DEFAULT 0 NOT NULL,
    meter_id character varying(50),
    filter_values jsonb,
    tier_mode character varying(20),
    tiers jsonb,
    price_unit_tiers jsonb,
    transform_quantity jsonb,
    lookup_key character varying(255),
    description text,
    metadata jsonb,
    entity_type character varying(20) DEFAULT 'PLAN'::character varying NOT NULL,
    entity_id character varying(50) NOT NULL,
    parent_price_id character varying(50),
    start_date timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    end_date timestamp with time zone,
    group_id character varying(50)
);


--
-- Name: subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscriptions (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    lookup_key character varying,
    customer_id character varying(50) NOT NULL,
    plan_id character varying(50) NOT NULL,
    subscription_status character varying(50) DEFAULT 'active'::character varying NOT NULL,
    currency character varying(10) NOT NULL,
    billing_anchor timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    start_date timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    end_date timestamp with time zone,
    current_period_start timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    current_period_end timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    cancelled_at timestamp with time zone,
    cancel_at timestamp with time zone,
    cancel_at_period_end boolean DEFAULT false NOT NULL,
    trial_start timestamp with time zone,
    trial_end timestamp with time zone,
    billing_cadence character varying NOT NULL,
    billing_period character varying NOT NULL,
    billing_period_count integer DEFAULT 1 NOT NULL,
    version bigint DEFAULT 1 NOT NULL,
    metadata jsonb,
    pause_status character varying(50) DEFAULT 'NONE'::character varying NOT NULL,
    active_pause_id character varying(50),
    billing_cycle character varying DEFAULT 'ANNIVERSARY'::character varying NOT NULL,
    commitment_amount numeric(20,6),
    overage_factor numeric(10,6) DEFAULT 1,
    payment_behavior character varying(50) DEFAULT 'DEFAULT_ACTIVE'::character varying NOT NULL,
    collection_method character varying(50) DEFAULT 'CHARGE_AUTOMATICALLY'::character varying NOT NULL,
    gateway_payment_method_id character varying(255),
    customer_timezone character varying DEFAULT 'UTC'::character varying NOT NULL,
    proration_behavior character varying DEFAULT 'NONE'::character varying NOT NULL,
    enable_true_up boolean DEFAULT false NOT NULL,
    invoicing_customer_id character varying(50)
);


--
-- Name: tenants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenants (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    name character varying(100) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    billing_details jsonb DEFAULT '{}'::jsonb,
    metadata jsonb
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    email character varying(255),
    type character varying DEFAULT 'user'::character varying NOT NULL,
    roles text[] DEFAULT '{}'::text[]
);


--
-- Name: wallet_transactions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.wallet_transactions (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    wallet_id character varying(50) NOT NULL,
    customer_id character varying(50),
    type character varying DEFAULT 'credit'::character varying NOT NULL,
    amount numeric(20,9) NOT NULL,
    credit_amount numeric(20,9) DEFAULT 0 NOT NULL,
    credit_balance_before numeric(20,9) DEFAULT 0 NOT NULL,
    credit_balance_after numeric(20,9) DEFAULT 0 NOT NULL,
    reference_type character varying(50),
    reference_id character varying,
    description character varying,
    metadata jsonb,
    transaction_status character varying(50) DEFAULT 'pending'::character varying NOT NULL,
    expiry_date timestamp with time zone,
    credits_available numeric(20,9) DEFAULT 0 NOT NULL,
    currency character varying(10) NOT NULL,
    conversion_rate numeric(10,5),
    topup_conversion_rate numeric(10,5),
    idempotency_key character varying,
    transaction_reason character varying(50) DEFAULT 'FREE_CREDIT'::character varying NOT NULL,
    priority integer
);


--
-- Name: wallets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.wallets (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying(255),
    customer_id character varying(50) NOT NULL,
    currency character varying(10) NOT NULL,
    description character varying,
    metadata jsonb,
    balance numeric(20,9) DEFAULT 0 NOT NULL,
    credit_balance numeric(20,9) DEFAULT 0 NOT NULL,
    wallet_status character varying(50) DEFAULT 'active'::character varying NOT NULL,
    auto_topup jsonb,
    wallet_type character varying(50) DEFAULT 'PRE_PAID'::character varying NOT NULL,
    conversion_rate numeric(10,5) DEFAULT 1 NOT NULL,
    topup_conversion_rate numeric(10,5) DEFAULT 1,
    config jsonb,
    alert_config jsonb,
    alert_enabled boolean DEFAULT true,
    alert_state character varying(50) DEFAULT 'OK'::character varying
);


--
-- Name: addons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.addons (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    lookup_key character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    type character varying(20) NOT NULL,
    metadata jsonb
);


--
-- Name: addon_associations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.addon_associations (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    entity_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    addon_id character varying(50) NOT NULL,
    start_date timestamp with time zone DEFAULT CURRENT_TIMESTAMP,
    end_date timestamp with time zone,
    addon_status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    cancellation_reason character varying(255),
    cancelled_at timestamp with time zone,
    metadata jsonb
);


--
-- Name: alert_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alert_logs (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(50) NOT NULL,
    parent_entity_type character varying(50),
    parent_entity_id character varying(50),
    customer_id character varying(50),
    alert_type character varying(50) NOT NULL,
    alert_status character varying(50) NOT NULL,
    alert_info jsonb NOT NULL
);


--
-- Name: billing_sequences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_sequences (
    tenant_id character varying(50) NOT NULL,
    subscription_id character varying(50) NOT NULL,
    last_sequence integer DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.connections (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying(255) NOT NULL,
    provider_type character varying(50) NOT NULL,
    encrypted_secret_data jsonb,
    metadata jsonb,
    sync_config jsonb
);


--
-- Name: costsheets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.costsheets (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying(255) NOT NULL,
    lookup_key character varying(255),
    description text,
    metadata jsonb
);


--
-- Name: coupon_associations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupon_associations (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    coupon_id character varying(50) NOT NULL,
    subscription_id character varying(50) NOT NULL,
    subscription_line_item_id character varying(50),
    subscription_phase_id character varying(50),
    start_date timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    end_date timestamp with time zone,
    metadata jsonb
);


--
-- Name: coupon_applications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupon_applications (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    coupon_id character varying(50) NOT NULL,
    coupon_association_id character varying(50),
    invoice_id character varying(50) NOT NULL,
    invoice_line_item_id character varying(50),
    applied_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    original_price numeric(20,8) NOT NULL,
    final_price numeric(20,8) NOT NULL,
    discounted_amount numeric(20,8) NOT NULL,
    discount_type character varying(20) NOT NULL,
    discount_percentage numeric(7,4),
    currency character varying(10),
    coupon_snapshot jsonb,
    metadata jsonb,
    subscription_id character varying(50)
);


--
-- Name: coupons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupons (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying(255) NOT NULL,
    redeem_after timestamp with time zone,
    redeem_before timestamp with time zone,
    max_redemptions integer,
    total_redemptions integer DEFAULT 0 NOT NULL,
    rules jsonb,
    amount_off numeric(20,8) DEFAULT 0,
    percentage_off numeric(7,4) DEFAULT 0,
    type character varying(20) DEFAULT 'fixed'::character varying NOT NULL,
    cadence character varying(20) DEFAULT 'once'::character varying NOT NULL,
    duration_in_periods integer,
    currency character varying(10),
    metadata jsonb
);


--
-- Name: credit_grant_applications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_grant_applications (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    credit_grant_id character varying(50) NOT NULL,
    subscription_id character varying(50) NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    applied_at timestamp with time zone,
    period_start timestamp with time zone NOT NULL,
    period_end timestamp with time zone,
    application_status character varying DEFAULT 'PENDING'::character varying NOT NULL,
    credits numeric(20,8) DEFAULT 0 NOT NULL,
    application_reason text NOT NULL,
    subscription_status_at_application character varying(50) NOT NULL,
    retry_count integer DEFAULT 0 NOT NULL,
    failure_reason text,
    metadata jsonb,
    idempotency_key character varying(100) NOT NULL
);


--
-- Name: credit_grants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_grants (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying(255) NOT NULL,
    scope character varying(50) NOT NULL,
    plan_id character varying(50),
    subscription_id character varying(50),
    credits numeric(20,8) DEFAULT 0 NOT NULL,
    conversion_rate numeric(10,5),
    topup_conversion_rate numeric(10,5),
    cadence character varying(50) NOT NULL,
    period character varying(50),
    period_count integer,
    expiration_type character varying(50) NOT NULL,
    expiration_duration integer,
    expiration_duration_unit character varying(50),
    priority integer,
    metadata jsonb DEFAULT '{}'::jsonb,
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    credit_grant_anchor timestamp with time zone
);


--
-- Name: credit_note_line_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_note_line_items (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    credit_note_id character varying(50) NOT NULL,
    invoice_line_item_id character varying(50) NOT NULL,
    display_name character varying NOT NULL,
    amount numeric(20,8) DEFAULT 0 NOT NULL,
    currency character varying(10) NOT NULL,
    metadata jsonb
);


--
-- Name: credit_notes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_notes (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    invoice_id character varying(50) NOT NULL,
    customer_id character varying(50) NOT NULL,
    subscription_id character varying(50),
    credit_note_number character varying(50) NOT NULL,
    credit_note_status character varying(50) DEFAULT 'DRAFT'::character varying NOT NULL,
    credit_note_type character varying(50) NOT NULL,
    refund_status character varying(50),
    reason character varying(50) NOT NULL,
    memo text NOT NULL,
    currency character varying(50) NOT NULL,
    idempotency_key character varying(100),
    voided_at timestamp with time zone,
    finalized_at timestamp with time zone,
    metadata jsonb,
    total_amount numeric(20,8) DEFAULT 0 NOT NULL
);


--
-- Name: entitlements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.entitlements (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    entity_type character varying(50) DEFAULT 'PLAN'::character varying,
    entity_id character varying(50),
    feature_id character varying(50) NOT NULL,
    feature_type character varying(50) NOT NULL,
    is_enabled boolean DEFAULT false NOT NULL,
    usage_limit bigint,
    usage_reset_period character varying(20),
    is_soft_limit boolean DEFAULT false NOT NULL,
    static_value character varying,
    display_order integer DEFAULT 0 NOT NULL,
    parent_entitlement_id character varying(50),
    start_date timestamp with time zone,
    end_date timestamp with time zone
);


--
-- Name: entity_integration_mappings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.entity_integration_mappings (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    entity_id character varying(255) NOT NULL,
    entity_type character varying(50) NOT NULL,
    provider_type character varying(50) NOT NULL,
    provider_entity_id character varying(255) NOT NULL,
    metadata jsonb
);


--
-- Name: features; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.features (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    lookup_key character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    type character varying(50) NOT NULL,
    meter_id character varying(50),
    metadata jsonb,
    unit_singular character varying(50),
    unit_plural character varying(50),
    alert_settings jsonb
);


--
-- Name: groups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.groups (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying(255) NOT NULL,
    entity_type character varying(50) DEFAULT 'price'::character varying NOT NULL,
    lookup_key character varying(255),
    metadata jsonb
);


--
-- Name: invoice_sequences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoice_sequences (
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50),
    year_month character varying(6) NOT NULL,
    last_value bigint DEFAULT 0 NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: payment_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payment_attempts (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    payment_id character varying(50) NOT NULL,
    payment_status character varying(20) NOT NULL,
    attempt_number integer DEFAULT 1 NOT NULL,
    gateway_attempt_id character varying(255),
    error_message text,
    metadata jsonb
);


--
-- Name: payments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    idempotency_key character varying(50) NOT NULL,
    destination_type character varying(50) NOT NULL,
    destination_id character varying(50) NOT NULL,
    payment_method_type character varying(50) NOT NULL,
    payment_method_id character varying(50),
    payment_gateway character varying(50),
    gateway_payment_id character varying(255),
    gateway_tracking_id character varying(255),
    gateway_metadata jsonb,
    amount numeric(20,8) DEFAULT 0 NOT NULL,
    currency character varying(10) NOT NULL,
    payment_status character varying(50) NOT NULL,
    track_attempts boolean DEFAULT false NOT NULL,
    metadata jsonb,
    succeeded_at timestamp with time zone,
    failed_at timestamp with time zone,
    refunded_at timestamp with time zone,
    recorded_at timestamp with time zone,
    error_message text
);


--
-- Name: price_units; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.price_units (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying(255) NOT NULL,
    code character varying(3) NOT NULL,
    symbol character varying(10) NOT NULL,
    base_currency character varying(3) NOT NULL,
    conversion_rate numeric(10,5) DEFAULT 1 NOT NULL,
    metadata jsonb
);


--
-- Name: scheduled_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scheduled_tasks (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    connection_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    interval character varying(20) NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    job_config jsonb,
    temporal_schedule_id character varying(100)
);


--
-- Name: secrets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secrets (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying NOT NULL,
    type character varying NOT NULL,
    provider character varying NOT NULL,
    value character varying,
    display_id character varying,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    provider_data jsonb,
    roles text[] DEFAULT '{}'::text[],
    user_type character varying DEFAULT 'user'::character varying
);


--
-- Name: settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.settings (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    key character varying NOT NULL,
    value jsonb DEFAULT '{}'::jsonb
);


--
-- Name: subscription_line_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_line_items (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    subscription_id character varying(50) NOT NULL,
    customer_id character varying(50) NOT NULL,
    entity_id character varying(50),
    entity_type character varying(50) DEFAULT 'PLAN'::character varying NOT NULL,
    plan_display_name character varying,
    price_id character varying(50) NOT NULL,
    price_type character varying(50),
    meter_id character varying(50),
    meter_display_name character varying,
    price_unit_id character varying(50),
    price_unit character varying(3),
    display_name character varying,
    quantity numeric(20,8) DEFAULT 0 NOT NULL,
    currency character varying(10) NOT NULL,
    billing_period character varying(50) NOT NULL,
    invoice_cadence character varying(20),
    trial_period integer DEFAULT 0 NOT NULL,
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    subscription_phase_id character varying(50),
    metadata jsonb,
    commitment_amount numeric(20,8),
    commitment_quantity numeric(20,8),
    commitment_type character varying(20),
    commitment_overage_factor numeric(10,4),
    commitment_true_up_enabled boolean DEFAULT false NOT NULL,
    commitment_windowed boolean DEFAULT false NOT NULL
);


--
-- Name: subscription_pauses; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_pauses (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    subscription_id character varying(50) NOT NULL,
    pause_status character varying(50) NOT NULL,
    pause_mode character varying(50) DEFAULT 'scheduled'::character varying NOT NULL,
    resume_mode character varying(50),
    pause_start timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    pause_end timestamp with time zone,
    resumed_at timestamp with time zone,
    original_period_start timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    original_period_end timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    reason text,
    metadata jsonb
);


--
-- Name: subscription_phases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_phases (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    subscription_id character varying(50) NOT NULL,
    start_date timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    end_date timestamp with time zone,
    metadata jsonb
);


--
-- Name: tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tasks (
    id character varying(100) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    task_type character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    scheduled_task_id character varying(50),
    workflow_id character varying(255),
    file_url character varying(255) DEFAULT ''::character varying NOT NULL,
    file_name character varying(255),
    file_type character varying(10) NOT NULL,
    task_status character varying(50) DEFAULT 'PENDING'::character varying NOT NULL,
    total_records integer,
    processed_records integer DEFAULT 0 NOT NULL,
    successful_records integer DEFAULT 0 NOT NULL,
    failed_records integer DEFAULT 0 NOT NULL,
    error_summary text,
    metadata jsonb,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    failed_at timestamp with time zone
);


--
-- Name: tax_applieds; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tax_applieds (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    tax_rate_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(50) NOT NULL,
    tax_association_id character varying(50),
    taxable_amount numeric(15,6),
    tax_amount numeric(15,6),
    currency character varying(3) NOT NULL,
    applied_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    metadata jsonb,
    idempotency_key character varying(50)
);


--
-- Name: tax_associations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tax_associations (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    tax_rate_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(50) NOT NULL,
    priority integer DEFAULT 100 NOT NULL,
    auto_apply boolean DEFAULT true NOT NULL,
    currency character varying(100),
    metadata jsonb
);


--
-- Name: tax_rates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tax_rates (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT '' NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying NOT NULL,
    description character varying,
    code character varying NOT NULL,
    tax_rate_status character varying NOT NULL,
    tax_rate_type character varying DEFAULT 'PERCENTAGE'::character varying NOT NULL,
    scope character varying NOT NULL,
    percentage_value numeric(9,6) DEFAULT 0,
    fixed_value numeric(9,6) DEFAULT 0,
    metadata jsonb
);


--
-- Name: auths auths_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auths
    ADD CONSTRAINT auths_pkey PRIMARY KEY (user_id);


--
-- Name: customers customers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.customers
    ADD CONSTRAINT customers_pkey PRIMARY KEY (id);


--
-- Name: environments environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environments
    ADD CONSTRAINT environments_pkey PRIMARY KEY (id);


--
-- Name: invoice_line_items invoice_line_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoice_line_items
    ADD CONSTRAINT invoice_line_items_pkey PRIMARY KEY (id);


--
-- Name: invoices invoices_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoices
    ADD CONSTRAINT invoices_pkey PRIMARY KEY (id);


--
-- Name: meters meters_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.meters
    ADD CONSTRAINT meters_pkey PRIMARY KEY (id);


--
-- Name: plans plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_pkey PRIMARY KEY (id);


--
-- Name: prices prices_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prices
    ADD CONSTRAINT prices_pkey PRIMARY KEY (id);


--
-- Name: subscriptions subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);


--
-- Name: tenants tenants_name_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_name_key UNIQUE (name);


--
-- Name: tenants tenants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (id);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: wallet_transactions wallet_transactions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.wallet_transactions
    ADD CONSTRAINT wallet_transactions_pkey PRIMARY KEY (id);


--
-- Name: wallets wallets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.wallets
    ADD CONSTRAINT wallets_pkey PRIMARY KEY (id);


--
-- Name: addons addons_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.addons
    ADD CONSTRAINT addons_pkey PRIMARY KEY (id);


--
-- Name: addon_associations addon_associations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.addon_associations
    ADD CONSTRAINT addon_associations_pkey PRIMARY KEY (id);


--
-- Name: alert_logs alert_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alert_logs
    ADD CONSTRAINT alert_logs_pkey PRIMARY KEY (id);


--
-- Name: billing_sequences billing_sequences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_sequences
    ADD CONSTRAINT billing_sequences_pkey PRIMARY KEY (tenant_id, subscription_id);


--
-- Name: connections connections_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.connections
    ADD CONSTRAINT connections_pkey PRIMARY KEY (id);


--
-- Name: costsheets costsheets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.costsheets
    ADD CONSTRAINT costsheets_pkey PRIMARY KEY (id);


--
-- Name: coupon_associations coupon_associations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_associations
    ADD CONSTRAINT coupon_associations_pkey PRIMARY KEY (id);


--
-- Name: coupon_applications coupon_applications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_applications
    ADD CONSTRAINT coupon_applications_pkey PRIMARY KEY (id);


--
-- Name: coupons coupons_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupons
    ADD CONSTRAINT coupons_pkey PRIMARY KEY (id);


--
-- Name: credit_grant_applications credit_grant_applications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_grant_applications
    ADD CONSTRAINT credit_grant_applications_pkey PRIMARY KEY (id);


--
-- Name: credit_grants credit_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_grants
    ADD CONSTRAINT credit_grants_pkey PRIMARY KEY (id);


--
-- Name: credit_note_line_items credit_note_line_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_note_line_items
    ADD CONSTRAINT credit_note_line_items_pkey PRIMARY KEY (id);


--
-- Name: credit_notes credit_notes_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_notes
    ADD CONSTRAINT credit_notes_pkey PRIMARY KEY (id);


--
-- Name: entitlements entitlements_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.entitlements
    ADD CONSTRAINT entitlements_pkey PRIMARY KEY (id);


--
-- Name: entity_integration_mappings entity_integration_mappings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.entity_integration_mappings
    ADD CONSTRAINT entity_integration_mappings_pkey PRIMARY KEY (id);


--
-- Name: features features_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.features
    ADD CONSTRAINT features_pkey PRIMARY KEY (id);


--
-- Name: groups groups_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.groups
    ADD CONSTRAINT groups_pkey PRIMARY KEY (id);


--
-- Name: invoice_sequences invoice_sequences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoice_sequences
    ADD CONSTRAINT invoice_sequences_pkey PRIMARY KEY (tenant_id, environment_id, year_month);


--
-- Name: payment_attempts payment_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payment_attempts
    ADD CONSTRAINT payment_attempts_pkey PRIMARY KEY (id);


--
-- Name: payments payments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_pkey PRIMARY KEY (id);


--
-- Name: price_units price_units_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_units
    ADD CONSTRAINT price_units_pkey PRIMARY KEY (id);


--
-- Name: scheduled_tasks scheduled_tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.scheduled_tasks
    ADD CONSTRAINT scheduled_tasks_pkey PRIMARY KEY (id);


--
-- Name: secrets secrets_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.secrets
    ADD CONSTRAINT secrets_pkey PRIMARY KEY (id);


--
-- Name: settings settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.settings
    ADD CONSTRAINT settings_pkey PRIMARY KEY (id);


--
-- Name: subscription_line_items subscription_line_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_line_items
    ADD CONSTRAINT subscription_line_items_pkey PRIMARY KEY (id);


--
-- Name: subscription_pauses subscription_pauses_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_pauses
    ADD CONSTRAINT subscription_pauses_pkey PRIMARY KEY (id);


--
-- Name: subscription_phases subscription_phases_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_phases
    ADD CONSTRAINT subscription_phases_pkey PRIMARY KEY (id);


--
-- Name: tasks tasks_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tasks
    ADD CONSTRAINT tasks_pkey PRIMARY KEY (id);


--
-- Name: tax_applieds tax_applieds_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tax_applieds
    ADD CONSTRAINT tax_applieds_pkey PRIMARY KEY (id);


--
-- Name: tax_associations tax_associations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tax_associations
    ADD CONSTRAINT tax_associations_pkey PRIMARY KEY (id);


--
-- Name: tax_rates tax_rates_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tax_rates
    ADD CONSTRAINT tax_rates_pkey PRIMARY KEY (id);






















--
-- Name: idx_customers_tenant_environment_external_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_customers_tenant_environment_external_id_unique ON public.customers USING btree (tenant_id, environment_id, external_id) WHERE ((external_id IS NOT NULL AND external_id != ''::character varying) AND status = 'published'::character varying);


--
-- Name: idx_customer_tenant_environment_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_customer_tenant_environment_email ON public.customers USING btree (tenant_id, environment_id, email) WHERE ((email IS NOT NULL AND email != ''::character varying) AND status = 'published'::character varying);


--
-- Name: idx_prices_tenant_environment_lookup_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_prices_tenant_environment_lookup_key_unique ON public.prices USING btree (tenant_id, environment_id, lookup_key) WHERE ((status = 'published'::character varying AND lookup_key IS NOT NULL AND lookup_key != ''::character varying) AND end_date IS NULL);


--
-- Name: idx_prices_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prices_tenant_environment ON public.prices USING btree (tenant_id, environment_id);


--
-- Name: idx_prices_start_date_end_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prices_start_date_end_date ON public.prices USING btree (start_date, end_date);


--
-- Name: idx_prices_tenant_environment_group_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prices_tenant_environment_group_id ON public.prices USING btree (tenant_id, environment_id, group_id);


--
-- Name: idx_invoices_tenant_environment_customer_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoices_tenant_environment_customer_status ON public.invoices USING btree (tenant_id, environment_id, customer_id, invoice_status, payment_status, status) WHERE (status = 'published'::character varying);


--
-- Name: idx_invoices_tenant_environment_subscription_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoices_tenant_environment_subscription_status ON public.invoices USING btree (tenant_id, environment_id, subscription_id, invoice_status, payment_status, status);


--
-- Name: idx_invoices_tenant_environment_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoices_tenant_environment_type_status ON public.invoices USING btree (tenant_id, environment_id, invoice_type, invoice_status, payment_status, status);


--
-- Name: idx_invoices_tenant_environment_due_date_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoices_tenant_environment_due_date_status ON public.invoices USING btree (tenant_id, environment_id, due_date, invoice_status, payment_status, status);


--
-- Name: idx_tenant_environment_invoice_number_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_invoice_number_unique ON public.invoices USING btree (tenant_id, environment_id, invoice_number) WHERE ((invoice_number IS NOT NULL AND invoice_number != ''::character varying) AND status = 'published'::character varying);


--
-- Name: idx_tenant_environment_idempotency_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_idempotency_key_unique ON public.invoices USING btree (tenant_id, environment_id, idempotency_key) WHERE (idempotency_key IS NOT NULL);


--
-- Name: idx_subscription_period_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_period_unique ON public.invoices USING btree (subscription_id, period_start, period_end) WHERE ((invoice_status != 'VOIDED'::character varying AND subscription_id IS NOT NULL));


--
-- Name: idx_invoice_line_items_tenant_environment_invoice_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoice_line_items_tenant_environment_invoice_status ON public.invoice_line_items USING btree (tenant_id, environment_id, invoice_id, status);


--
-- Name: idx_invoice_line_items_tenant_environment_subscription_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoice_line_items_tenant_environment_subscription_status ON public.invoice_line_items USING btree (tenant_id, environment_id, subscription_id, status);


--
-- Name: idx_invoice_line_items_subscription_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_invoice_line_items_subscription_status ON public.invoice_line_items USING btree (subscription_id, status);


--
-- Name: idx_wallet_transactions_tenant_environment_wallet; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallet_transactions_tenant_environment_wallet ON public.wallet_transactions USING btree (tenant_id, environment_id, wallet_id);


--
-- Name: idx_wallet_transactions_tenant_environment_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallet_transactions_tenant_environment_customer ON public.wallet_transactions USING btree (tenant_id, environment_id, customer_id);


--
-- Name: idx_wallet_transactions_tenant_environment_reference; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallet_transactions_tenant_environment_reference ON public.wallet_transactions USING btree (tenant_id, environment_id, reference_type, reference_id, status);


--
-- Name: idx_wallet_transactions_tenant_environment_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallet_transactions_tenant_environment_created_at ON public.wallet_transactions USING btree (tenant_id, environment_id, created_at);


--
-- Name: idx_tenant_wallet_type_credits_available_expiry_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_wallet_type_credits_available_expiry_date ON public.wallet_transactions USING btree (tenant_id, environment_id, wallet_id, type, credits_available, expiry_date) WHERE ((credits_available > 0 AND type = 'credit'::character varying));


--
-- Name: idx_tenant_environment_idempotency_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_idempotency_key ON public.wallet_transactions USING btree (tenant_id, environment_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL AND idempotency_key <> ''::character varying AND status = 'published'::character varying));


--
-- Name: idx_wallets_tenant_environment_customer_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallets_tenant_environment_customer_status ON public.wallets USING btree (tenant_id, environment_id, customer_id, status);


--
-- Name: idx_wallets_tenant_environment_status_wallet_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallets_tenant_environment_status_wallet_status ON public.wallets USING btree (tenant_id, environment_id, status, wallet_status);


--
-- Name: idx_subscriptions_tenant_environment_customer_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_tenant_environment_customer_status ON public.subscriptions USING btree (tenant_id, environment_id, customer_id, status) WHERE (status = 'published'::character varying);


--
-- Name: idx_subscriptions_tenant_environment_plan_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_tenant_environment_plan_status ON public.subscriptions USING btree (tenant_id, environment_id, plan_id, status);


--
-- Name: idx_subscriptions_tenant_environment_subscription_status_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_tenant_environment_subscription_status_status ON public.subscriptions USING btree (tenant_id, environment_id, subscription_status, status);


--
-- Name: idx_subscriptions_tenant_environment_current_period_end; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_tenant_environment_current_period_end ON public.subscriptions USING btree (tenant_id, environment_id, current_period_end, subscription_status, status);


--
-- Name: idx_plans_tenant_environment_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_plans_tenant_environment_lookup_key ON public.plans USING btree (tenant_id, environment_id, lookup_key) WHERE ((status = 'published'::character varying AND lookup_key IS NOT NULL AND lookup_key != ''::character varying));


--
-- Name: idx_plans_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plans_tenant_environment ON public.plans USING btree (tenant_id, environment_id);


--
-- Name: idx_meters_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_meters_tenant_environment ON public.meters USING btree (tenant_id, environment_id);


--
-- Name: idx_environments_tenant_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_environments_tenant_type ON public.environments USING btree (tenant_id, type);


--
-- Name: idx_environments_tenant_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_environments_tenant_status ON public.environments USING btree (tenant_id, status);


--
-- Name: idx_environments_tenant_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_environments_tenant_created_at ON public.environments USING btree (tenant_id, created_at);


--
-- Name: idx_users_email_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_users_email_unique ON public.users USING btree (email) WHERE ((status = 'published'::character varying AND email IS NOT NULL AND email != ''::character varying));


--
-- Name: idx_user_tenant_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_tenant_status ON public.users USING btree (tenant_id, status, type);


--
-- Name: idx_user_tenant_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_tenant_created_at ON public.users USING btree (tenant_id, created_at);


--
-- Name: idx_tenant_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_created_at ON public.tenants USING btree (created_at);


--
-- Name: idx_auth_user_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_auth_user_id_unique ON public.auths USING btree (user_id) WHERE (status = 'published'::character varying);


--
-- Name: idx_auth_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_provider ON public.auths USING btree (provider);


--
-- Name: idx_auth_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_status ON public.auths USING btree (status);


--
-- Name: idx_auth_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_created_at ON public.auths USING btree (created_at);


--
-- Name: idx_addons_tenant_environment_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_addons_tenant_environment_lookup_key ON public.addons USING btree (tenant_id, environment_id, lookup_key) WHERE ((status = 'published'::character varying AND lookup_key IS NOT NULL AND lookup_key != ''::character varying));


--
-- Name: idx_addon_associations_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_addon_associations_tenant_environment ON public.addon_associations USING btree (tenant_id, environment_id, entity_id, entity_type, addon_id);


--
-- Name: idx_alert_logs_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alert_logs_entity ON public.alert_logs USING btree (tenant_id, environment_id, entity_type, entity_id);


--
-- Name: idx_alert_logs_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alert_logs_type ON public.alert_logs USING btree (tenant_id, environment_id, alert_type);


--
-- Name: idx_alert_logs_entity_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alert_logs_entity_created_at ON public.alert_logs USING btree (tenant_id, environment_id, entity_type, entity_id, created_at);


--
-- Name: idx_alert_logs_entity_parent_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alert_logs_entity_parent_created_at ON public.alert_logs USING btree (tenant_id, environment_id, entity_type, entity_id, parent_entity_type, parent_entity_id, created_at);


--
-- Name: idx_alert_logs_customer_type_status_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alert_logs_customer_type_status_created_at ON public.alert_logs USING btree (tenant_id, environment_id, customer_id, alert_type, alert_status, created_at);


--
-- Name: idx_connections_tenant_environment_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_connections_tenant_environment_provider ON public.connections USING btree (tenant_id, environment_id, provider_type);


--
-- Name: idx_costsheets_tenant_environment_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_costsheets_tenant_environment_lookup_key ON public.costsheets USING btree (tenant_id, environment_id, lookup_key) WHERE ((status = 'published'::character varying AND lookup_key IS NOT NULL AND lookup_key != ''::character varying));


--
-- Name: idx_costsheets_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_costsheets_tenant_environment ON public.costsheets USING btree (tenant_id, environment_id);


--
-- Name: idx_coupon_associations_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_associations_tenant_environment ON public.coupon_associations USING btree (tenant_id, environment_id);


--
-- Name: idx_coupon_associations_tenant_environment_coupon; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_associations_tenant_environment_coupon ON public.coupon_associations USING btree (tenant_id, environment_id, coupon_id);


--
-- Name: idx_coupon_associations_tenant_environment_subscription; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_associations_tenant_environment_subscription ON public.coupon_associations USING btree (tenant_id, environment_id, subscription_id);


--
-- Name: idx_coupon_associations_tenant_environment_subscription_line_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_associations_tenant_environment_subscription_line_item ON public.coupon_associations USING btree (tenant_id, environment_id, subscription_id, subscription_line_item_id);


--
-- Name: idx_coupon_applications_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_applications_tenant_environment ON public.coupon_applications USING btree (tenant_id, environment_id);


--
-- Name: idx_coupon_applications_tenant_environment_coupon; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_applications_tenant_environment_coupon ON public.coupon_applications USING btree (tenant_id, environment_id, coupon_id);


--
-- Name: idx_coupon_applications_tenant_environment_invoice; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_applications_tenant_environment_invoice ON public.coupon_applications USING btree (tenant_id, environment_id, invoice_id);


--
-- Name: idx_coupon_applications_tenant_environment_invoice_line_item; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_applications_tenant_environment_invoice_line_item ON public.coupon_applications USING btree (tenant_id, environment_id, invoice_id, invoice_line_item_id);


--
-- Name: idx_coupon_applications_tenant_environment_subscription; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_applications_tenant_environment_subscription ON public.coupon_applications USING btree (tenant_id, environment_id, subscription_id);


--
-- Name: idx_coupon_applications_tenant_environment_subscription_coupon; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupon_applications_tenant_environment_subscription_coupon ON public.coupon_applications USING btree (tenant_id, environment_id, subscription_id, coupon_id);


--
-- Name: idx_coupons_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_coupons_tenant_environment ON public.coupons USING btree (tenant_id, environment_id);


--
-- Name: idx_credit_grant_applications_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_grant_applications_tenant_environment ON public.credit_grant_applications USING btree (tenant_id, environment_id);


--
-- Name: idx_credit_grants_tenant_environment_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_grants_tenant_environment_status ON public.credit_grants USING btree (tenant_id, environment_id, status);


--
-- Name: idx_plan_id_not_null; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plan_id_not_null ON public.credit_grants USING btree (tenant_id, environment_id, scope, plan_id) WHERE (plan_id IS NOT NULL);


--
-- Name: idx_subscription_id_not_null; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_id_not_null ON public.credit_grants USING btree (tenant_id, environment_id, scope, subscription_id) WHERE (subscription_id IS NOT NULL);


--
-- Name: idx_credit_notes_tenant_environment_credit_note_number; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_credit_notes_tenant_environment_credit_note_number ON public.credit_notes USING btree (tenant_id, environment_id, credit_note_number) WHERE ((credit_note_number IS NOT NULL AND credit_note_number != ''::character varying AND status = 'published'::character varying));


--
-- Name: idx_credit_notes_tenant_environment_idempotency_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_notes_tenant_environment_idempotency_key ON public.credit_notes USING btree (tenant_id, environment_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL AND idempotency_key != ''::character varying));


--
-- Name: idx_credit_notes_tenant_environment_invoice; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_notes_tenant_environment_invoice ON public.credit_notes USING btree (tenant_id, environment_id, invoice_id);


--
-- Name: idx_credit_notes_tenant_environment_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_notes_tenant_environment_status ON public.credit_notes USING btree (tenant_id, environment_id, credit_note_status);


--
-- Name: idx_credit_notes_tenant_environment_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_notes_tenant_environment_type ON public.credit_notes USING btree (tenant_id, environment_id, credit_note_type);


--
-- Name: idx_credit_notes_tenant_environment_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_notes_tenant_environment_customer ON public.credit_notes USING btree (tenant_id, environment_id, customer_id);


--
-- Name: idx_credit_notes_tenant_environment_subscription; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_credit_notes_tenant_environment_subscription ON public.credit_notes USING btree (tenant_id, environment_id, subscription_id);


--
-- Name: idx_entitlements_tenant_environment_entity_feature_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_entitlements_tenant_environment_entity_feature_unique ON public.entitlements USING btree (tenant_id, environment_id, entity_type, entity_id, feature_id) WHERE (status = 'published'::character varying);


--
-- Name: idx_entitlements_tenant_environment_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entitlements_tenant_environment_entity ON public.entitlements USING btree (tenant_id, environment_id, entity_type, entity_id);


--
-- Name: idx_entitlements_tenant_environment_feature; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entitlements_tenant_environment_feature ON public.entitlements USING btree (tenant_id, environment_id, feature_id);


--
-- Name: idx_entitlements_tenant_environment_parent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entitlements_tenant_environment_parent ON public.entitlements USING btree (tenant_id, environment_id, parent_entitlement_id);


--
-- Name: idx_entitlements_entity_feature_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entitlements_entity_feature_time ON public.entitlements USING btree (entity_id, entity_type, feature_id, start_date, end_date) WHERE ((entity_type = 'SUBSCRIPTION'::character varying AND status = 'published'::character varying));


--
-- Name: idx_entity_integration_mappings_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_entity_integration_mappings_unique ON public.entity_integration_mappings USING btree (tenant_id, environment_id, entity_type, entity_id, provider_type) WHERE (status = 'published'::character varying);


--
-- Name: idx_entity_integration_mappings_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entity_integration_mappings_provider ON public.entity_integration_mappings USING btree (provider_type, provider_entity_id);


--
-- Name: idx_feature_tenant_env_lookup_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_feature_tenant_env_lookup_key_unique ON public.features USING btree (tenant_id, environment_id, lookup_key) WHERE (((lookup_key IS NOT NULL AND lookup_key != ''::character varying) AND status = 'published'::character varying));


--
-- Name: idx_feature_tenant_env_meter_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_meter_id ON public.features USING btree (tenant_id, environment_id, meter_id) WHERE (meter_id IS NOT NULL);


--
-- Name: idx_feature_tenant_env_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_type ON public.features USING btree (tenant_id, environment_id, type);


--
-- Name: idx_feature_tenant_env_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_status ON public.features USING btree (tenant_id, environment_id, status);


--
-- Name: idx_feature_tenant_env_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_created_at ON public.features USING btree (tenant_id, environment_id, created_at);


--
-- Name: idx_group_tenant_environment_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_group_tenant_environment_lookup_key ON public.groups USING btree (tenant_id, environment_id, lookup_key) WHERE ((status = 'published'::character varying AND lookup_key IS NOT NULL AND lookup_key != ''::character varying));


--
-- Name: idx_groups_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_groups_tenant_environment ON public.groups USING btree (tenant_id, environment_id);


--
-- Name: idx_price_units_tenant_environment_code_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_price_units_tenant_environment_code_unique ON public.price_units USING btree (tenant_id, environment_id, code) WHERE (status = 'published'::character varying);


--
-- Name: idx_scheduled_tasks_tenant_environment_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_tasks_tenant_environment_enabled ON public.scheduled_tasks USING btree (tenant_id, environment_id, enabled);


--
-- Name: idx_scheduled_tasks_connection_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_tasks_connection_enabled ON public.scheduled_tasks USING btree (connection_id, enabled);


--
-- Name: idx_scheduled_tasks_entity_interval_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_tasks_entity_interval_enabled ON public.scheduled_tasks USING btree (entity_type, interval, enabled);


--
-- Name: idx_scheduled_tasks_connection_entity_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_scheduled_tasks_connection_entity_status ON public.scheduled_tasks USING btree (connection_id, entity_type, status);


--
-- Name: idx_secrets_type_value_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_secrets_type_value_status ON public.secrets USING btree (type, value, status);


--
-- Name: idx_secrets_tenant_environment_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_secrets_tenant_environment_type_status ON public.secrets USING btree (tenant_id, environment_id, type, status);


--
-- Name: idx_secrets_tenant_environment_provider_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_secrets_tenant_environment_provider_status ON public.secrets USING btree (tenant_id, environment_id, provider, status);


--
-- Name: idx_settings_tenant_environment_status_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_settings_tenant_environment_status_key_unique ON public.settings USING btree (tenant_id, environment_id, status, key);


--
-- Name: idx_subscription_line_items_tenant_environment_subscription_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_tenant_environment_subscription_status ON public.subscription_line_items USING btree (tenant_id, environment_id, subscription_id, status);


--
-- Name: idx_subscription_line_items_tenant_environment_customer_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_tenant_environment_customer_status ON public.subscription_line_items USING btree (tenant_id, environment_id, customer_id, status);


--
-- Name: idx_subscription_line_items_tenant_environment_entity_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_tenant_environment_entity_status ON public.subscription_line_items USING btree (tenant_id, environment_id, entity_id, entity_type, status);


--
-- Name: idx_subscription_line_items_tenant_environment_price_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_tenant_environment_price_status ON public.subscription_line_items USING btree (tenant_id, environment_id, price_id, status);


--
-- Name: idx_subscription_line_items_tenant_environment_meter_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_tenant_environment_meter_status ON public.subscription_line_items USING btree (tenant_id, environment_id, meter_id, status);


--
-- Name: idx_subscription_line_items_start_end_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_start_end_date ON public.subscription_line_items USING btree (start_date, end_date);


--
-- Name: idx_subscription_line_items_subscription_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_subscription_status ON public.subscription_line_items USING btree (subscription_id, status);


--
-- Name: idx_subscription_pauses_tenant_environment_subscription_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_pauses_tenant_environment_subscription_status ON public.subscription_pauses USING btree (tenant_id, environment_id, subscription_id, status);


--
-- Name: idx_subscription_pauses_tenant_environment_pause_start_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_pauses_tenant_environment_pause_start_status ON public.subscription_pauses USING btree (tenant_id, environment_id, pause_start, status);


--
-- Name: idx_subscription_pauses_tenant_environment_pause_end_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_pauses_tenant_environment_pause_end_status ON public.subscription_pauses USING btree (tenant_id, environment_id, pause_end, status);


--
-- Name: idx_subscription_phases_tenant_environment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_phases_tenant_environment ON public.subscription_phases USING btree (tenant_id, environment_id);


--
-- Name: idx_tasks_tenant_env_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_tenant_env_type_status ON public.tasks USING btree (tenant_id, environment_id, task_type, entity_type, status);


--
-- Name: idx_tasks_tenant_env_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_tenant_env_user ON public.tasks USING btree (tenant_id, environment_id, created_by, status);


--
-- Name: idx_tasks_tenant_env_task_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_tenant_env_task_status ON public.tasks USING btree (tenant_id, environment_id, task_status, status);


--
-- Name: idx_entity_tax_rate_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_entity_tax_rate_id ON public.tax_applieds USING btree (tenant_id, environment_id, entity_type, entity_id, tax_rate_id);


--
-- Name: idx_entity_tax_association_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entity_tax_association_lookup ON public.tax_applieds USING btree (tenant_id, environment_id, entity_type, entity_id);


--
-- Name: idx_entity_lookup_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entity_lookup_active ON public.tax_associations USING btree (tenant_id, environment_id, entity_type, entity_id, status);


--
-- Name: idx_tax_rate_id_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tax_rate_id_tenant_id_environment_id ON public.tax_associations USING btree (tenant_id, environment_id, tax_rate_id);


--
-- Name: unique_entity_tax_mapping; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX unique_entity_tax_mapping ON public.tax_associations USING btree (tenant_id, environment_id, entity_type, entity_id, tax_rate_id);


--
-- Name: idx_code_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_code_tenant_id_environment_id ON public.tax_rates USING btree (tenant_id, environment_id, code) WHERE (((code IS NOT NULL AND code != ''::character varying) AND status = 'published'::character varying));


--
-- Name: idx_payment_attempt_number_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_payment_attempt_number_unique ON public.payment_attempts USING btree (payment_id, attempt_number);


--
-- Name: idx_payment_attempt_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_payment_attempt_status ON public.payment_attempts USING btree (payment_id, status);


--
-- Name: idx_gateway_attempt; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_gateway_attempt ON public.payment_attempts USING btree (gateway_attempt_id) WHERE (gateway_attempt_id IS NOT NULL);


--
-- Name: idx_tenant_destination_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_destination_status ON public.payments USING btree (tenant_id, environment_id, destination_type, destination_id, payment_status, status);


--
-- Name: idx_tenant_payment_method_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_payment_method_status ON public.payments USING btree (tenant_id, environment_id, payment_method_type, payment_method_id, payment_status, status);


--
-- Name: idx_tenant_gateway_payment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_gateway_payment ON public.payments USING btree (tenant_id, environment_id, payment_gateway, gateway_payment_id) WHERE ((payment_gateway IS NOT NULL AND gateway_payment_id IS NOT NULL));


--
-- PostgreSQL database dump complete
--

