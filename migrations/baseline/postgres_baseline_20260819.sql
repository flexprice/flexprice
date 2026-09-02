--
-- PostgreSQL database dump
--


-- Dumped from database version 16.6
-- Dumped by pg_dump version 18.4

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
-- SET transaction_timeout = 0;  -- PG17+ only; prod is PG 16.6, this line is not portable
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

CREATE SCHEMA IF NOT EXISTS public;  -- IF NOT EXISTS added: a stock database already has this schema


--
-- Name: SCHEMA public; Type: COMMENT; Schema: -; Owner: -
--

COMMENT ON SCHEMA public IS 'standard public schema';


--
-- Name: cleanup_invoice_sequences(); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.cleanup_invoice_sequences() RETURNS void
    LANGUAGE plpgsql
    AS $$
BEGIN
    DELETE FROM invoice_sequences
    WHERE year_month < to_char(current_date - interval '1 year', 'YYYYMM');
END;
$$;


--
-- Name: next_billing_sequence(character varying, character varying); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.next_billing_sequence(p_tenant_id character varying, p_subscription_id character varying) RETURNS integer
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_next_val INTEGER;
BEGIN
    INSERT INTO billing_sequences (tenant_id, subscription_id, last_sequence)
    VALUES (p_tenant_id, p_subscription_id, 1)
    ON CONFLICT (tenant_id, subscription_id)
    DO UPDATE SET 
        last_sequence = billing_sequences.last_sequence + 1,
        updated_at = CURRENT_TIMESTAMP
    RETURNING last_sequence INTO v_next_val;
    
    RETURN v_next_val;
END;
$$;


--
-- Name: next_invoice_sequence(character varying, character varying); Type: FUNCTION; Schema: public; Owner: -
--

CREATE FUNCTION public.next_invoice_sequence(p_tenant_id character varying, p_year_month character varying) RETURNS bigint
    LANGUAGE plpgsql
    AS $$
DECLARE
    v_next_val BIGINT;
BEGIN
    INSERT INTO invoice_sequences (tenant_id, year_month, last_value)
    VALUES (p_tenant_id, p_year_month, 1)
    ON CONFLICT (tenant_id, year_month)
    DO UPDATE SET 
        last_value = invoice_sequences.last_value + 1,
        updated_at = CURRENT_TIMESTAMP
    RETURNING last_value INTO v_next_val;
    
    RETURN v_next_val;
END;
$$;


SET default_table_access_method = heap;

--
-- Name: addon_associations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.addon_associations (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    entity_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    addon_id character varying(50) NOT NULL,
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    addon_status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    cancellation_reason character varying(255),
    cancelled_at timestamp with time zone,
    metadata jsonb
);


--
-- Name: addons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.addons (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    lookup_key character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    type character varying(20) DEFAULT 'onetime'::character varying NOT NULL,
    metadata jsonb
);


--
-- Name: alert_logs; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alert_logs (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(50) NOT NULL,
    alert_type character varying(50) NOT NULL,
    alert_status character varying(50) NOT NULL,
    alert_info jsonb NOT NULL,
    parent_entity_type character varying(50),
    parent_entity_id character varying(50),
    customer_id character varying(50),
    alert_setting_id character varying(50)
);


--
-- Name: alert_settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.alert_settings (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    enabled boolean DEFAULT true NOT NULL,
    entity_type character varying NOT NULL,
    entity_id character varying(50) NOT NULL,
    parent_entity_type character varying,
    parent_entity_id character varying(50),
    config jsonb NOT NULL
);


--
-- Name: auths; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.auths (
    user_id character varying(50) NOT NULL,
    provider character varying(20) NOT NULL,
    token text NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    id bigint NOT NULL
);


--
-- Name: auths_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.auths ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.auths_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: billing_sequences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.billing_sequences (
    tenant_id character varying(50) NOT NULL,
    subscription_id character varying(50) NOT NULL,
    last_sequence integer DEFAULT 0 NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    id bigint NOT NULL
);


--
-- Name: billing_sequences_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.billing_sequences ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.billing_sequences_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: checkout_sessions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.checkout_sessions (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    customer_id character varying(50) NOT NULL,
    action character varying(30) NOT NULL,
    checkout_status character varying(20) DEFAULT 'initiated'::character varying NOT NULL,
    payment_provider character varying(20) NOT NULL,
    checkout_invoice_id character varying(50),
    checkout_payment_id character varying(50),
    configuration jsonb NOT NULL,
    result jsonb,
    provider_result jsonb,
    idempotency_key character varying(255),
    success_url text,
    failure_url text,
    cancel_url text,
    expires_at timestamp with time zone,
    completed_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    failure_reason text,
    metadata jsonb,
    payment_provider_config jsonb
);


--
-- Name: connections; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.connections (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
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
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    metadata jsonb,
    name character varying(255) NOT NULL,
    lookup_key character varying(255),
    description text,
    price_costsheet character varying(50)
);


--
-- Name: coupon_applications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupon_applications (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    coupon_association_id character varying(50),
    applied_at timestamp with time zone NOT NULL,
    original_price numeric(20,8) NOT NULL,
    final_price numeric(20,8) NOT NULL,
    discounted_amount numeric(20,8) NOT NULL,
    discount_type character varying(20) NOT NULL,
    discount_percentage numeric(7,4),
    currency character varying(10),
    coupon_snapshot jsonb,
    metadata jsonb,
    coupon_id character varying(50) NOT NULL,
    invoice_id character varying(50) NOT NULL,
    invoice_line_item_id character varying(50),
    subscription_id character varying(50)
);


--
-- Name: coupon_association_coupon_applications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupon_association_coupon_applications (
    coupon_association_id character varying(50) NOT NULL,
    coupon_application_id character varying(50) NOT NULL
);


--
-- Name: coupon_associations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupon_associations (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    metadata jsonb,
    coupon_id character varying(50) NOT NULL,
    subscription_id character varying(50) NOT NULL,
    subscription_line_item_id character varying(50),
    subscription_phase_id character varying(50),
    start_date timestamp with time zone NOT NULL,
    end_date timestamp with time zone
);


--
-- Name: coupons; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.coupons (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    name character varying(255) NOT NULL,
    redeem_after timestamp with time zone,
    redeem_before timestamp with time zone,
    max_redemptions bigint,
    total_redemptions bigint DEFAULT 0 NOT NULL,
    rules jsonb,
    amount_off numeric(20,8),
    percentage_off numeric(7,4),
    type character varying(20) DEFAULT 'fixed'::character varying NOT NULL,
    cadence character varying(20) DEFAULT 'once'::character varying NOT NULL,
    duration_in_periods bigint,
    currency character varying(10),
    metadata jsonb,
    coupon_code character varying(100)
);


--
-- Name: credit_grant_applications; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_grant_applications (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    credit_grant_id character varying(50) NOT NULL,
    subscription_id character varying(50) NOT NULL,
    scheduled_for timestamp with time zone NOT NULL,
    applied_at timestamp with time zone,
    application_status character varying DEFAULT 'pending'::character varying NOT NULL,
    application_reason text NOT NULL,
    subscription_status_at_application character varying(50) NOT NULL,
    retry_count bigint DEFAULT 0 NOT NULL,
    failure_reason text,
    metadata jsonb,
    period_start timestamp with time zone NOT NULL,
    period_end timestamp with time zone,
    credits numeric(20,8) NOT NULL,
    idempotency_key character varying(100) NOT NULL
);


--
-- Name: credit_grants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_grants (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    name character varying(255) NOT NULL,
    scope character varying(50) NOT NULL,
    credits numeric(20,8) NOT NULL,
    cadence character varying(50) NOT NULL,
    period character varying(50),
    period_count bigint,
    expiration_type character varying(50) NOT NULL,
    expiration_duration bigint,
    expiration_duration_unit character varying(50),
    priority bigint,
    metadata jsonb,
    plan_id character varying(50),
    subscription_id character varying(50),
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    credit_grant_anchor timestamp with time zone,
    conversion_rate numeric(10,5),
    topup_conversion_rate numeric(10,5),
    addon_id character varying(50)
);


--
-- Name: credit_note_line_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_note_line_items (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    invoice_line_item_id character varying(50) NOT NULL,
    display_name character varying NOT NULL,
    amount numeric(20,8) NOT NULL,
    currency character varying(10) NOT NULL,
    metadata jsonb,
    credit_note_id character varying(50) NOT NULL
);


--
-- Name: credit_notes; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.credit_notes (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
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
    total_amount numeric(20,8) NOT NULL
);


--
-- Name: customers; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.customers (
    id character varying(50) NOT NULL,
    external_id character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    email character varying(255),
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    address_line1 character varying(255),
    address_line2 character varying(255),
    address_city character varying(100),
    address_state character varying(100),
    address_postal_code character varying(20),
    address_country character varying(2),
    metadata jsonb,
    environment_id character varying(50) DEFAULT ''::character varying,
    parent_customer_id character varying(50),
    timezone character varying(50) DEFAULT 'UTC'::character varying,
    contact character varying(20)
);


--
-- Name: entitlement_grants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.entitlement_grants (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    entitlement_config_id character varying(50) NOT NULL,
    customer_id character varying(50) NOT NULL,
    subscription_id character varying(50) NOT NULL,
    scope_entity_type character varying(20) DEFAULT 'feature'::character varying NOT NULL,
    scope_entity_id character varying(50) NOT NULL,
    measure character varying(20) NOT NULL,
    quota numeric(25,15) NOT NULL,
    usage numeric(25,15) NOT NULL,
    valid_from timestamp with time zone NOT NULL,
    valid_to timestamp with time zone NOT NULL,
    grant_status character varying(20) DEFAULT 'active'::character varying NOT NULL,
    last_computed_at timestamp with time zone,
    quota_crossed_at timestamp with time zone
);


--
-- Name: entitlements; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.entitlements (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    feature_id character varying(50) NOT NULL,
    feature_type character varying(50) NOT NULL,
    is_enabled boolean DEFAULT false NOT NULL,
    usage_limit bigint,
    usage_reset_period character varying(20),
    is_soft_limit boolean DEFAULT false NOT NULL,
    static_value character varying,
    plan_id character varying(50),
    environment_id character varying(50) DEFAULT ''::character varying,
    entity_type character varying(50) DEFAULT 'PLAN'::character varying,
    entity_id character varying(50),
    addon_entitlements character varying(50),
    display_order bigint DEFAULT 0 NOT NULL,
    parent_entitlement_id character varying(50),
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    config_value jsonb,
    grant_measure character varying(20),
    grant_duration_value bigint,
    grant_duration_unit character varying(20),
    grant_quota numeric(25,15),
    aggregation_mode character varying(20) DEFAULT 'additive'::character varying NOT NULL,
    grant_allocation_behavior character varying(20) DEFAULT 'first_usage'::character varying
);


--
-- Name: entity_integration_mappings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.entity_integration_mappings (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    entity_id character varying(255) NOT NULL,
    entity_type character varying(50) NOT NULL,
    provider_type character varying(50) NOT NULL,
    provider_entity_id character varying(255) NOT NULL,
    metadata jsonb
);


--
-- Name: environments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.environments (
    id character varying(50) NOT NULL,
    name character varying(50) NOT NULL,
    type character varying(20) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying
);


--
-- Name: features; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.features (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
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
    environment_id character varying(50) DEFAULT ''::character varying,
    alert_settings jsonb,
    reporting_unit_singular character varying(255),
    reporting_unit_plural character varying(255),
    reporting_unit_conversion_rate numeric(20,10),
    group_id character varying(50)
);


--
-- Name: groups; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.groups (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    metadata jsonb,
    name character varying(255) NOT NULL,
    entity_type character varying(50) DEFAULT 'price'::character varying NOT NULL,
    lookup_key character varying(255)
);


--
-- Name: incoming_webhook_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.incoming_webhook_events (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    provider character varying(50) NOT NULL,
    method character varying(10) NOT NULL,
    path text NOT NULL,
    request_id character varying(100),
    headers jsonb,
    body text
);


--
-- Name: invoice_line_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoice_line_items (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    customer_id character varying(50) NOT NULL,
    subscription_id character varying(50),
    price_id character varying(50),
    meter_id character varying(50),
    amount numeric(20,8) NOT NULL,
    quantity numeric(20,8) NOT NULL,
    currency character varying(10) NOT NULL,
    period_start timestamp with time zone,
    period_end timestamp with time zone,
    metadata jsonb,
    invoice_id character varying(50) NOT NULL,
    plan_id character varying(50),
    plan_display_name character varying,
    price_type character varying(50),
    meter_display_name character varying,
    display_name character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    price_unit_id character varying(50),
    price_unit character varying(3),
    price_unit_amount numeric(20,8),
    entity_id character varying(50),
    entity_type character varying(50),
    commitment_info jsonb,
    prepaid_credits_applied numeric(20,8),
    line_item_discount numeric(20,8),
    invoice_level_discount numeric(20,8),
    subscription_line_item_id character varying(50),
    adjusted_entitlement_quantity numeric(20,8),
    parent_line_item_id character varying(50)
);


--
-- Name: invoice_sequences; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoice_sequences (
    tenant_id character varying(50) NOT NULL,
    year_month character varying(6) NOT NULL,
    last_value bigint DEFAULT 0 NOT NULL,
    created_at timestamp without time zone NOT NULL,
    updated_at timestamp without time zone NOT NULL,
    id bigint NOT NULL,
    environment_id character varying(50)
);


--
-- Name: invoice_sequences_id_seq; Type: SEQUENCE; Schema: public; Owner: -
--

ALTER TABLE public.invoice_sequences ALTER COLUMN id ADD GENERATED BY DEFAULT AS IDENTITY (
    SEQUENCE NAME public.invoice_sequences_id_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1
);


--
-- Name: invoices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoices (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
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
    amount_due numeric(20,8) NOT NULL,
    amount_paid numeric(20,8) NOT NULL,
    amount_remaining numeric(20,8) NOT NULL,
    description character varying,
    due_date timestamp with time zone,
    paid_at timestamp with time zone,
    voided_at timestamp with time zone,
    finalized_at timestamp with time zone,
    invoice_pdf_url character varying,
    billing_reason character varying,
    metadata jsonb,
    version bigint DEFAULT 1 NOT NULL,
    period_start timestamp with time zone,
    period_end timestamp with time zone,
    invoice_number character varying(50),
    billing_sequence integer,
    idempotency_key character varying(100),
    billing_period character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    subtotal numeric(20,8),
    total numeric(20,8),
    adjustment_amount numeric(20,8),
    refunded_amount numeric(20,8),
    total_discount numeric(20,8),
    total_tax numeric(20,8),
    total_prepaid_credits_applied numeric(20,8),
    recalculated_invoice_id character varying,
    last_computed_at timestamp with time zone,
    subscription_customer_id character varying(50),
    issue_date timestamp with time zone,
    is_manually_edited boolean DEFAULT false NOT NULL
);


--
-- Name: meters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.meters (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    event_name character varying(255) NOT NULL,
    aggregation jsonb NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    filters jsonb NOT NULL,
    reset_usage character varying(20) DEFAULT 'BILLING_PERIOD'::character varying NOT NULL,
    name character varying(255) NOT NULL,
    environment_id character varying(50) DEFAULT ''::character varying
);


--
-- Name: payment_attempts; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payment_attempts (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    payment_status character varying(20) NOT NULL,
    attempt_number integer DEFAULT 1 NOT NULL,
    gateway_attempt_id character varying(255),
    error_message text,
    metadata jsonb,
    payment_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT ''::character varying
);


--
-- Name: payment_methods; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payment_methods (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    customer_id character varying(50) NOT NULL,
    type character varying(50) NOT NULL,
    gateway character varying(50) NOT NULL,
    gateway_method_id character varying(255) NOT NULL,
    payment_method_status character varying(50) DEFAULT 'ACTIVE'::character varying NOT NULL,
    is_default boolean DEFAULT false NOT NULL,
    method_details jsonb
);


--
-- Name: payments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.payments (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    idempotency_key character varying(50) NOT NULL,
    destination_type character varying(50) NOT NULL,
    destination_id character varying(50) NOT NULL,
    payment_method_type character varying(50) NOT NULL,
    payment_method_id character varying(50),
    payment_gateway character varying(50),
    gateway_payment_id character varying(255),
    amount numeric(20,8) NOT NULL,
    currency character varying(10) NOT NULL,
    payment_status character varying(50) NOT NULL,
    track_attempts boolean DEFAULT false NOT NULL,
    metadata jsonb,
    succeeded_at timestamp with time zone,
    failed_at timestamp with time zone,
    refunded_at timestamp with time zone,
    error_message text,
    environment_id character varying(50) DEFAULT ''::character varying,
    recorded_at timestamp with time zone,
    gateway_tracking_id character varying(255),
    gateway_metadata jsonb,
    voided_at timestamp with time zone
);


--
-- Name: plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plans (
    id character varying(50) NOT NULL,
    lookup_key character varying(255),
    name character varying(255) NOT NULL,
    description text,
    invoice_cadence character varying(20),
    trial_period bigint DEFAULT 0,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    metadata jsonb,
    display_order bigint DEFAULT 0 NOT NULL
);


--
-- Name: price_unit; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.price_unit (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    name character varying(255) NOT NULL,
    code character varying(3) NOT NULL,
    symbol character varying(10) NOT NULL,
    base_currency character varying(3) NOT NULL,
    conversion_rate numeric(10,5) NOT NULL,
    "precision" bigint DEFAULT 0 NOT NULL
);


--
-- Name: price_units; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.price_units (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    metadata jsonb,
    name character varying(255) NOT NULL,
    code character varying(3) NOT NULL,
    symbol character varying(10) NOT NULL,
    base_currency character varying(3) NOT NULL,
    conversion_rate numeric(10,5) NOT NULL
);


--
-- Name: prices_sequence_seq; Type: SEQUENCE; Schema: public; Owner: -
--

CREATE SEQUENCE public.prices_sequence_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


--
-- Name: prices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prices (
    id character varying(50) NOT NULL,
    amount numeric(25,15) NOT NULL,
    currency character varying(3) NOT NULL,
    display_amount character varying(255),
    plan_id character varying(50),
    type character varying(20) NOT NULL,
    billing_period character varying(20) NOT NULL,
    billing_period_count bigint NOT NULL,
    billing_model character varying(20) NOT NULL,
    billing_cadence character varying(20) DEFAULT 'RECURRING'::character varying NOT NULL,
    meter_id character varying(50),
    filter_values jsonb,
    tier_mode character varying(20),
    tiers jsonb,
    transform_quantity jsonb,
    lookup_key character varying(255),
    description text,
    metadata jsonb,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    invoice_cadence character varying(20),
    trial_period bigint DEFAULT 0 NOT NULL,
    price_unit_type character varying(20) DEFAULT 'FIAT'::character varying NOT NULL,
    price_unit character varying(3),
    price_unit_amount numeric(25,15),
    display_price_unit_amount character varying(255),
    conversion_rate numeric(25,15),
    price_unit_tiers jsonb,
    scope character varying(20) DEFAULT 'PLAN'::character varying NOT NULL,
    parent_price_id character varying(50),
    subscription_id character varying(50),
    price_unit_id character varying(50),
    entity_type character varying(20) DEFAULT 'PLAN'::character varying NOT NULL,
    entity_id character varying(50) NOT NULL,
    addon_prices character varying(50),
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    group_id character varying(50),
    display_name character varying(255),
    min_quantity numeric(20,8),
    trial_period_days bigint DEFAULT 0 NOT NULL,
    sequence bigint DEFAULT nextval('public.prices_sequence_seq'::regclass) NOT NULL
);


--
-- Name: refunds; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.refunds (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    payment_id character varying(50) NOT NULL,
    payment_gateway character varying(50) NOT NULL,
    gateway_refund_id character varying(255),
    gateway_tracking_id character varying(255),
    amount numeric(20,8) NOT NULL,
    currency character varying(10) NOT NULL,
    refund_status character varying(50) NOT NULL,
    refund_reason character varying(50) NOT NULL,
    idempotency_key character varying(255) NOT NULL,
    gateway_idempotency_token character varying(255) NOT NULL,
    failure_reason text,
    metadata jsonb,
    gateway_metadata jsonb,
    initiated_at timestamp with time zone,
    succeeded_at timestamp with time zone,
    failed_at timestamp with time zone,
    cancelled_at timestamp with time zone
);


--
-- Name: scheduled_tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.scheduled_tasks (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    connection_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    "interval" character varying(20) NOT NULL,
    enabled boolean DEFAULT true NOT NULL,
    job_config jsonb,
    temporal_schedule_id character varying(100)
);


--
-- Name: secrets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.secrets (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    name character varying NOT NULL,
    type character varying NOT NULL,
    provider character varying NOT NULL,
    value character varying,
    display_id character varying,
    permissions jsonb,
    expires_at timestamp with time zone,
    last_used_at timestamp with time zone,
    provider_data jsonb,
    environment_id character varying(50) DEFAULT ''::character varying,
    roles jsonb,
    user_type character varying DEFAULT 'user'::character varying,
    user_id character varying
);


--
-- Name: settings; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.settings (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    key character varying NOT NULL,
    value jsonb
);


--
-- Name: subscription_line_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_line_items (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    customer_id character varying(50) NOT NULL,
    plan_id character varying(50),
    plan_display_name character varying,
    price_id character varying(50) NOT NULL,
    price_type character varying(50),
    meter_id character varying(50),
    meter_display_name character varying,
    display_name character varying,
    quantity numeric(20,8) NOT NULL,
    currency character varying(10) NOT NULL,
    billing_period character varying(50) NOT NULL,
    start_date timestamp with time zone,
    end_date timestamp with time zone,
    metadata jsonb,
    subscription_id character varying(50) NOT NULL,
    environment_id character varying(50) DEFAULT ''::character varying,
    invoice_cadence character varying(20),
    trial_period bigint DEFAULT 0 NOT NULL,
    price_unit_id character varying(50),
    price_unit character varying(3),
    entity_id character varying(50),
    entity_type character varying(50) DEFAULT 'plan'::character varying NOT NULL,
    subscription_phase_id character varying(50),
    commitment_amount numeric(20,8),
    commitment_quantity numeric(20,8),
    commitment_type character varying(20),
    commitment_overage_factor numeric(10,4),
    commitment_true_up_enabled boolean DEFAULT false NOT NULL,
    commitment_windowed boolean DEFAULT false NOT NULL,
    commitment_duration character varying(50),
    billing_period_count bigint DEFAULT 1 NOT NULL,
    addon_association_id character varying(50),
    commitment_time_buckets jsonb
);


--
-- Name: subscription_pauses; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_pauses (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    pause_status character varying(50) NOT NULL,
    pause_mode character varying(50) DEFAULT 'scheduled'::character varying NOT NULL,
    resume_mode character varying(50),
    pause_start timestamp with time zone NOT NULL,
    pause_end timestamp with time zone,
    resumed_at timestamp with time zone,
    original_period_start timestamp with time zone NOT NULL,
    original_period_end timestamp with time zone NOT NULL,
    reason text,
    metadata jsonb,
    subscription_id character varying(50) NOT NULL
);


--
-- Name: subscription_phases; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_phases (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    metadata jsonb,
    start_date timestamp with time zone NOT NULL,
    end_date timestamp with time zone,
    subscription_id character varying(50) NOT NULL
);


--
-- Name: subscription_schedules; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscription_schedules (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    metadata jsonb,
    schedule_type character varying(50) NOT NULL,
    scheduled_at timestamp with time zone NOT NULL,
    configuration jsonb NOT NULL,
    executed_at timestamp with time zone,
    cancelled_at timestamp with time zone,
    execution_result jsonb,
    error_message text,
    subscription_id character varying(50) NOT NULL
);


--
-- Name: subscriptions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.subscriptions (
    id character varying(50) NOT NULL,
    lookup_key character varying,
    customer_id character varying(50) NOT NULL,
    plan_id character varying(50) NOT NULL,
    subscription_status character varying(50) DEFAULT 'active'::character varying NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    currency character varying(10) NOT NULL,
    billing_anchor timestamp with time zone NOT NULL,
    start_date timestamp with time zone NOT NULL,
    end_date timestamp with time zone,
    current_period_start timestamp with time zone NOT NULL,
    current_period_end timestamp with time zone NOT NULL,
    cancelled_at timestamp with time zone,
    cancel_at timestamp with time zone,
    cancel_at_period_end boolean DEFAULT false NOT NULL,
    trial_start timestamp with time zone,
    trial_end timestamp with time zone,
    invoice_cadence character varying,
    billing_cadence character varying NOT NULL,
    billing_period character varying NOT NULL,
    billing_period_count bigint DEFAULT 1 NOT NULL,
    tenant_id character varying(50) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    version bigint DEFAULT 1 NOT NULL,
    metadata jsonb,
    environment_id character varying(50) DEFAULT ''::character varying,
    pause_status character varying(50) DEFAULT 'none'::character varying NOT NULL,
    active_pause_id character varying(50),
    billing_cycle character varying DEFAULT 'anniversary'::character varying NOT NULL,
    commitment_amount numeric(20,6),
    overage_factor numeric(10,6),
    payment_behavior character varying(50) DEFAULT 'default_active'::character varying NOT NULL,
    collection_method character varying(50) DEFAULT 'charge_automatically'::character varying NOT NULL,
    gateway_payment_method_id character varying(255),
    customer_timezone character varying DEFAULT 'UTC'::character varying NOT NULL,
    proration_behavior character varying DEFAULT 'none'::character varying NOT NULL,
    enable_true_up boolean DEFAULT false NOT NULL,
    invoicing_customer_id character varying(50),
    commitment_duration character varying(50),
    parent_subscription_id character varying(50),
    payment_terms character varying(20),
    subscription_type character varying(20) DEFAULT 'standalone'::character varying NOT NULL,
    auto_invoice_threshold numeric(20,6),
    synced_price_sequence bigint DEFAULT '0'::bigint NOT NULL,
    timezone character varying DEFAULT 'UTC'::character varying NOT NULL
);


--
-- Name: system_events; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.system_events (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    entity_type character varying(64) DEFAULT ''::character varying,
    entity_id character varying(50) DEFAULT ''::character varying,
    webhook_message_id character varying(128),
    published_at timestamp with time zone,
    payload jsonb,
    event_name character varying(128) DEFAULT ''::character varying,
    failure_reason text,
    failure_count bigint DEFAULT 0 NOT NULL
);


--
-- Name: tasks; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tasks (
    id character varying(100) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    task_type character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    file_url character varying(255) DEFAULT ''::character varying NOT NULL,
    file_type character varying(10) NOT NULL,
    task_status character varying(50) DEFAULT 'PENDING'::character varying NOT NULL,
    total_records bigint,
    processed_records bigint DEFAULT 0 NOT NULL,
    successful_records bigint DEFAULT 0 NOT NULL,
    failed_records bigint DEFAULT 0 NOT NULL,
    error_summary text,
    metadata jsonb,
    started_at timestamp with time zone,
    completed_at timestamp with time zone,
    failed_at timestamp with time zone,
    file_name character varying(255),
    environment_id character varying(50) DEFAULT ''::character varying,
    scheduled_task_id character varying(50),
    workflow_id character varying(255)
);


--
-- Name: tax_applieds; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tax_applieds (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    tax_rate_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(50) NOT NULL,
    tax_association_id character varying(50),
    taxable_amount numeric(15,6) NOT NULL,
    tax_amount numeric(15,6) NOT NULL,
    currency character varying(3) NOT NULL,
    applied_at timestamp with time zone NOT NULL,
    metadata jsonb,
    idempotency_key character varying(50)
);


--
-- Name: tax_associations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tax_associations (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    tax_rate_id character varying(50) NOT NULL,
    entity_type character varying(50) NOT NULL,
    entity_id character varying(50) NOT NULL,
    priority integer DEFAULT 100 NOT NULL,
    auto_apply boolean DEFAULT true NOT NULL,
    currency character varying(100),
    metadata jsonb,
    start_date timestamp with time zone,
    end_date timestamp with time zone
);


--
-- Name: tax_rates; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tax_rates (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    name character varying NOT NULL,
    description character varying,
    code character varying NOT NULL,
    tax_rate_status character varying NOT NULL,
    tax_rate_type character varying DEFAULT 'percentage'::character varying NOT NULL,
    scope character varying NOT NULL,
    percentage_value numeric(9,6),
    fixed_value numeric(9,6),
    metadata jsonb
);


--
-- Name: tenants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenants (
    id character varying(50) NOT NULL,
    name character varying(100) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    billing_details jsonb,
    metadata jsonb,
    internal_status character varying(20) DEFAULT 'trialing'::character varying NOT NULL
);


--
-- Name: usage_records; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.usage_records (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    customer_id character varying(50) NOT NULL,
    customer_external_id character varying(255),
    subscription_id character varying(50) NOT NULL,
    plan_id character varying(50) NOT NULL,
    quantity numeric(20,8) NOT NULL,
    amount numeric(20,8) NOT NULL,
    period_start timestamp with time zone NOT NULL,
    period_end timestamp with time zone NOT NULL,
    syncs jsonb,
    currency character varying(10) NOT NULL,
    synced boolean DEFAULT false NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id character varying(50) NOT NULL,
    email character varying(255),
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    type character varying DEFAULT 'user'::character varying NOT NULL,
    roles jsonb,
    metadata jsonb,
    name character varying(255)
);


--
-- Name: wallet_transactions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.wallet_transactions (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    wallet_id character varying(50) NOT NULL,
    type character varying DEFAULT 'credit'::character varying NOT NULL,
    amount numeric(20,9) NOT NULL,
    transaction_status character varying(50) DEFAULT 'pending'::character varying NOT NULL,
    reference_type character varying(50),
    reference_id character varying,
    description character varying,
    metadata jsonb,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    transaction_type character varying DEFAULT 'credit'::character varying NOT NULL,
    expiry_date timestamp without time zone,
    amount_used numeric(20,8) DEFAULT '0'::numeric NOT NULL,
    transaction_reason character varying(50) DEFAULT 'FREE_CREDIT_GRANT'::character varying NOT NULL,
    credit_amount numeric(20,9) DEFAULT '0'::numeric NOT NULL,
    credit_balance_before numeric(20,9) DEFAULT '0'::numeric NOT NULL,
    credit_balance_after numeric(20,9) DEFAULT '0'::numeric NOT NULL,
    balance_before numeric(20,9),
    balance_after numeric(20,9),
    credits_available numeric(20,9) NOT NULL,
    environment_id character varying(50) DEFAULT ''::character varying,
    idempotency_key character varying,
    priority bigint,
    customer_id character varying(50),
    currency character varying(10) NOT NULL,
    conversion_rate numeric(10,5),
    topup_conversion_rate numeric(10,5),
    parent_transaction_id character varying(50)
);


--
-- Name: wallets; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.wallets (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    customer_id character varying(50) NOT NULL,
    currency character varying(10) NOT NULL,
    balance numeric(20,9) NOT NULL,
    wallet_status character varying(50) DEFAULT 'active'::character varying NOT NULL,
    metadata jsonb,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    description character varying,
    name character varying(255),
    auto_topup_trigger character varying(50) DEFAULT 'disabled'::character varying,
    auto_topup_min_balance numeric(20,9),
    auto_topup_amount numeric(20,9),
    wallet_type character varying(50) DEFAULT 'PRE_PAID'::character varying NOT NULL,
    config jsonb,
    credit_balance numeric(20,9) DEFAULT '0'::numeric NOT NULL,
    conversion_rate numeric(10,5) NOT NULL,
    environment_id character varying(50) DEFAULT ''::character varying,
    alert_config jsonb,
    alert_enabled boolean DEFAULT true,
    alert_state character varying(50) DEFAULT 'ok'::character varying,
    auto_topup jsonb,
    topup_conversion_rate numeric(10,5),
    alert_settings jsonb
);


--
-- Name: workflow_executions; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.workflow_executions (
    id character varying(26) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    environment_id character varying(50) DEFAULT ''::character varying,
    workflow_id character varying(255) NOT NULL,
    run_id character varying(255) NOT NULL,
    workflow_type character varying(100) NOT NULL,
    task_queue character varying(100) NOT NULL,
    start_time timestamp with time zone NOT NULL,
    metadata jsonb,
    end_time timestamp with time zone,
    duration_ms bigint,
    workflow_status character varying(50) DEFAULT 'Running'::character varying NOT NULL,
    entity character varying(100),
    entity_id character varying(255)
);


--
-- Name: addon_associations addon_associations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.addon_associations
    ADD CONSTRAINT addon_associations_pkey PRIMARY KEY (id);


--
-- Name: addons addons_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.addons
    ADD CONSTRAINT addons_pkey PRIMARY KEY (id);


--
-- Name: alert_logs alert_logs_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alert_logs
    ADD CONSTRAINT alert_logs_pkey PRIMARY KEY (id);


--
-- Name: alert_settings alert_settings_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.alert_settings
    ADD CONSTRAINT alert_settings_pkey PRIMARY KEY (id);


--
-- Name: auths auths_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.auths
    ADD CONSTRAINT auths_pkey PRIMARY KEY (id);


--
-- Name: billing_sequences billing_sequences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.billing_sequences
    ADD CONSTRAINT billing_sequences_pkey PRIMARY KEY (id);


--
-- Name: checkout_sessions checkout_sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.checkout_sessions
    ADD CONSTRAINT checkout_sessions_pkey PRIMARY KEY (id);


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
-- Name: coupon_applications coupon_applications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_applications
    ADD CONSTRAINT coupon_applications_pkey PRIMARY KEY (id);


--
-- Name: coupon_association_coupon_applications coupon_association_coupon_applications_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_association_coupon_applications
    ADD CONSTRAINT coupon_association_coupon_applications_pkey PRIMARY KEY (coupon_association_id, coupon_application_id);


--
-- Name: coupon_associations coupon_associations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_associations
    ADD CONSTRAINT coupon_associations_pkey PRIMARY KEY (id);


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
-- Name: customers customers_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.customers
    ADD CONSTRAINT customers_pkey PRIMARY KEY (id);


--
-- Name: entitlement_grants entitlement_grants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.entitlement_grants
    ADD CONSTRAINT entitlement_grants_pkey PRIMARY KEY (id);


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
-- Name: environments environments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.environments
    ADD CONSTRAINT environments_pkey PRIMARY KEY (id);


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
-- Name: incoming_webhook_events incoming_webhook_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.incoming_webhook_events
    ADD CONSTRAINT incoming_webhook_events_pkey PRIMARY KEY (id);


--
-- Name: invoice_line_items invoice_line_items_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoice_line_items
    ADD CONSTRAINT invoice_line_items_pkey PRIMARY KEY (id);


--
-- Name: invoice_sequences invoice_sequences_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoice_sequences
    ADD CONSTRAINT invoice_sequences_pkey PRIMARY KEY (id);


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
-- Name: payment_attempts payment_attempts_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payment_attempts
    ADD CONSTRAINT payment_attempts_pkey PRIMARY KEY (id);


--
-- Name: payment_methods payment_methods_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payment_methods
    ADD CONSTRAINT payment_methods_pkey PRIMARY KEY (id);


--
-- Name: payments payments_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payments
    ADD CONSTRAINT payments_pkey PRIMARY KEY (id);


--
-- Name: plans plans_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.plans
    ADD CONSTRAINT plans_pkey PRIMARY KEY (id);


--
-- Name: price_unit price_unit_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_unit
    ADD CONSTRAINT price_unit_pkey PRIMARY KEY (id);


--
-- Name: price_units price_units_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.price_units
    ADD CONSTRAINT price_units_pkey PRIMARY KEY (id);


--
-- Name: prices prices_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prices
    ADD CONSTRAINT prices_pkey PRIMARY KEY (id);


--
-- Name: refunds refunds_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.refunds
    ADD CONSTRAINT refunds_pkey PRIMARY KEY (id);


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
-- Name: subscription_schedules subscription_schedules_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_schedules
    ADD CONSTRAINT subscription_schedules_pkey PRIMARY KEY (id);


--
-- Name: subscriptions subscriptions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_pkey PRIMARY KEY (id);


--
-- Name: system_events system_events_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.system_events
    ADD CONSTRAINT system_events_pkey PRIMARY KEY (id);


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
-- Name: tenants tenants_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.tenants
    ADD CONSTRAINT tenants_pkey PRIMARY KEY (id);


--
-- Name: usage_records usage_records_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.usage_records
    ADD CONSTRAINT usage_records_pkey PRIMARY KEY (id);


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
-- Name: workflow_executions workflow_executions_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.workflow_executions
    ADD CONSTRAINT workflow_executions_pkey PRIMARY KEY (id);


--
-- Name: addon_tenant_id_environment_id_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX addon_tenant_id_environment_id_lookup_key ON public.addons USING btree (tenant_id, environment_id, lookup_key) WHERE (((status)::text = 'published'::text) AND (lookup_key IS NOT NULL) AND ((lookup_key)::text <> ''::text));


--
-- Name: addonassociation_tenant_id_env_9ee9df30de004854d4600926c7fc899e; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX addonassociation_tenant_id_env_9ee9df30de004854d4600926c7fc899e ON public.addon_associations USING btree (tenant_id, environment_id, entity_id, entity_type, addon_id);


--
-- Name: billingsequence_tenant_id_subscription_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX billingsequence_tenant_id_subscription_id ON public.billing_sequences USING btree (tenant_id, subscription_id);


--
-- Name: connection_tenant_id_environment_id_provider_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX connection_tenant_id_environment_id_provider_type ON public.connections USING btree (tenant_id, environment_id, provider_type);


--
-- Name: costsheet_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX costsheet_tenant_id_environment_id ON public.costsheets USING btree (tenant_id, environment_id);


--
-- Name: coupon_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX coupon_tenant_id_environment_id ON public.coupons USING btree (tenant_id, environment_id);


--
-- Name: couponapplication_tenant_id_en_0bf629c2124d5dccd084975591956692; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponapplication_tenant_id_en_0bf629c2124d5dccd084975591956692 ON public.coupon_applications USING btree (tenant_id, environment_id, subscription_id, coupon_id);


--
-- Name: couponapplication_tenant_id_en_c2dc969c48465c63c60b58f6af4bdb19; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponapplication_tenant_id_en_c2dc969c48465c63c60b58f6af4bdb19 ON public.coupon_applications USING btree (tenant_id, environment_id, invoice_id, invoice_line_item_id);


--
-- Name: couponapplication_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponapplication_tenant_id_environment_id ON public.coupon_applications USING btree (tenant_id, environment_id);


--
-- Name: couponapplication_tenant_id_environment_id_coupon_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponapplication_tenant_id_environment_id_coupon_id ON public.coupon_applications USING btree (tenant_id, environment_id, coupon_id);


--
-- Name: couponapplication_tenant_id_environment_id_invoice_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponapplication_tenant_id_environment_id_invoice_id ON public.coupon_applications USING btree (tenant_id, environment_id, invoice_id);


--
-- Name: couponapplication_tenant_id_environment_id_subscription_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponapplication_tenant_id_environment_id_subscription_id ON public.coupon_applications USING btree (tenant_id, environment_id, subscription_id);


--
-- Name: couponassociation_tenant_id_en_3a2d66edd7f97fd99fc16f545dc8d6b1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponassociation_tenant_id_en_3a2d66edd7f97fd99fc16f545dc8d6b1 ON public.coupon_associations USING btree (tenant_id, environment_id, subscription_id, subscription_line_item_id);


--
-- Name: couponassociation_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponassociation_tenant_id_environment_id ON public.coupon_associations USING btree (tenant_id, environment_id);


--
-- Name: couponassociation_tenant_id_environment_id_coupon_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponassociation_tenant_id_environment_id_coupon_id ON public.coupon_associations USING btree (tenant_id, environment_id, coupon_id);


--
-- Name: couponassociation_tenant_id_environment_id_subscription_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX couponassociation_tenant_id_environment_id_subscription_id ON public.coupon_associations USING btree (tenant_id, environment_id, subscription_id);


--
-- Name: credit_grant_applications_idempotency_key_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX credit_grant_applications_idempotency_key_key ON public.credit_grant_applications USING btree (idempotency_key);


--
-- Name: creditgrant_tenant_id_environment_id_scope_plan_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditgrant_tenant_id_environment_id_scope_plan_id ON public.credit_grants USING btree (tenant_id, environment_id, scope, plan_id);


--
-- Name: creditgrant_tenant_id_environment_id_scope_subscription_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditgrant_tenant_id_environment_id_scope_subscription_id ON public.credit_grants USING btree (tenant_id, environment_id, scope, subscription_id);


--
-- Name: creditgrant_tenant_id_environment_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditgrant_tenant_id_environment_id_status ON public.credit_grants USING btree (tenant_id, environment_id, status);


--
-- Name: creditnote_tenant_id_environment_id_credit_note_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditnote_tenant_id_environment_id_credit_note_status ON public.credit_notes USING btree (tenant_id, environment_id, credit_note_status);


--
-- Name: creditnote_tenant_id_environment_id_credit_note_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditnote_tenant_id_environment_id_credit_note_type ON public.credit_notes USING btree (tenant_id, environment_id, credit_note_type);


--
-- Name: creditnote_tenant_id_environment_id_customer_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditnote_tenant_id_environment_id_customer_id ON public.credit_notes USING btree (tenant_id, environment_id, customer_id);


--
-- Name: creditnote_tenant_id_environment_id_idempotency_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditnote_tenant_id_environment_id_idempotency_key ON public.credit_notes USING btree (tenant_id, environment_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL) AND ((idempotency_key)::text <> ''::text));


--
-- Name: creditnote_tenant_id_environment_id_invoice_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditnote_tenant_id_environment_id_invoice_id ON public.credit_notes USING btree (tenant_id, environment_id, invoice_id);


--
-- Name: creditnote_tenant_id_environment_id_subscription_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX creditnote_tenant_id_environment_id_subscription_id ON public.credit_notes USING btree (tenant_id, environment_id, subscription_id);


--
-- Name: customer_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX customer_tenant_id_environment_id ON public.customers USING btree (tenant_id, environment_id);


--
-- Name: entitlement_entity_id_entity_t_24e490ce36f12474ef30194a8dd3e18d; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX entitlement_entity_id_entity_t_24e490ce36f12474ef30194a8dd3e18d ON public.entitlements USING btree (entity_id, entity_type, feature_id, start_date, end_date) WHERE (((entity_type)::text = 'SUBSCRIPTION'::text) AND ((status)::text = 'published'::text));


--
-- Name: entitlement_tenant_id_environm_4be9d447f26ab17e315682af3a45d8ea; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX entitlement_tenant_id_environm_4be9d447f26ab17e315682af3a45d8ea ON public.entitlements USING btree (tenant_id, environment_id, entity_type, entity_id, feature_id) WHERE ((status)::text = 'published'::text);


--
-- Name: entitlement_tenant_id_environment_id_entity_type_entity_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX entitlement_tenant_id_environment_id_entity_type_entity_id ON public.entitlements USING btree (tenant_id, environment_id, entity_type, entity_id);


--
-- Name: entitlement_tenant_id_environment_id_entity_type_entity_id_feat; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX entitlement_tenant_id_environment_id_entity_type_entity_id_feat ON public.entitlements USING btree (tenant_id, environment_id, entity_type, entity_id, feature_id) WHERE (((status)::text = 'published'::text) AND ((aggregation_mode)::text <> 'parallel'::text));


--
-- Name: entitlement_tenant_id_environment_id_feature_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX entitlement_tenant_id_environment_id_feature_id ON public.entitlements USING btree (tenant_id, environment_id, feature_id);


--
-- Name: entitlement_tenant_id_environment_id_parent_entitlement_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX entitlement_tenant_id_environment_id_parent_entitlement_id ON public.entitlements USING btree (tenant_id, environment_id, parent_entitlement_id);


--
-- Name: entitlement_tenant_id_environment_id_plan_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX entitlement_tenant_id_environment_id_plan_id ON public.entitlements USING btree (tenant_id, environment_id, plan_id);


--
-- Name: entitlement_tenant_id_environment_id_plan_id_feature_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX entitlement_tenant_id_environment_id_plan_id_feature_id ON public.entitlements USING btree (tenant_id, environment_id, plan_id, feature_id) WHERE ((status)::text = 'published'::text);


--
-- Name: entitlementgrant_tenant_id_env_8274f32268cfe6c520b9e2791fbf4b8d; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX entitlementgrant_tenant_id_env_8274f32268cfe6c520b9e2791fbf4b8d ON public.entitlement_grants USING btree (tenant_id, environment_id, customer_id, valid_to, entitlement_config_id, subscription_id);


--
-- Name: entitlementgrant_tenant_id_env_add69fd6dc00d6b9bb459096fbafb46d; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX entitlementgrant_tenant_id_env_add69fd6dc00d6b9bb459096fbafb46d ON public.entitlement_grants USING btree (tenant_id, environment_id, entitlement_config_id, customer_id, subscription_id, valid_from);


--
-- Name: entityintegrationmapping_provider_type_provider_entity_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX entityintegrationmapping_provider_type_provider_entity_id ON public.entity_integration_mappings USING btree (provider_type, provider_entity_id);


--
-- Name: group_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX group_tenant_id_environment_id ON public.groups USING btree (tenant_id, environment_id);


--
-- Name: idx_addon_id_not_null; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_addon_id_not_null ON public.credit_grants USING btree (tenant_id, environment_id, scope, addon_id) WHERE (addon_id IS NOT NULL);


--
-- Name: idx_alert_settings_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alert_settings_entity ON public.alert_settings USING btree (tenant_id, environment_id, status, enabled, entity_type, entity_id);


--
-- Name: idx_alert_settings_parent; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alert_settings_parent ON public.alert_settings USING btree (tenant_id, environment_id, status, enabled, entity_type, parent_entity_type, parent_entity_id);


--
-- Name: idx_alertlogs_alert_setting_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alertlogs_alert_setting_id ON public.alert_logs USING btree (tenant_id, environment_id, alert_setting_id, created_at);


--
-- Name: idx_alertlogs_customer_type_status_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alertlogs_customer_type_status_created_at ON public.alert_logs USING btree (tenant_id, environment_id, customer_id, alert_type, alert_status, created_at);


--
-- Name: idx_alertlogs_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alertlogs_entity ON public.alert_logs USING btree (tenant_id, environment_id, entity_type, entity_id);


--
-- Name: idx_alertlogs_entity_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alertlogs_entity_created_at ON public.alert_logs USING btree (tenant_id, environment_id, entity_type, entity_id, created_at);


--
-- Name: idx_alertlogs_entity_parent_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alertlogs_entity_parent_created_at ON public.alert_logs USING btree (tenant_id, environment_id, entity_type, entity_id, parent_entity_type, parent_entity_id, created_at);


--
-- Name: idx_alertlogs_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_alertlogs_type ON public.alert_logs USING btree (tenant_id, environment_id, alert_type);


--
-- Name: idx_auth_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_created_at ON public.auths USING btree (created_at);


--
-- Name: idx_auth_provider; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_provider ON public.auths USING btree (provider);


--
-- Name: idx_auth_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_auth_status ON public.auths USING btree (status);


--
-- Name: idx_auth_user_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_auth_user_id_unique ON public.auths USING btree (user_id) WHERE ((status)::text = 'published'::text);


--
-- Name: idx_checkout_session_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_checkout_session_customer ON public.checkout_sessions USING btree (tenant_id, environment_id, customer_id);


--
-- Name: idx_checkout_session_expiry; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_checkout_session_expiry ON public.checkout_sessions USING btree (expires_at) WHERE ((checkout_status)::text = ANY ((ARRAY['initiated'::character varying, 'pending'::character varying])::text[]));


--
-- Name: idx_checkout_session_idempotency_key_active; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_checkout_session_idempotency_key_active ON public.checkout_sessions USING btree (tenant_id, environment_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL) AND ((checkout_status)::text = ANY ((ARRAY['initiated'::character varying, 'pending'::character varying])::text[])));


--
-- Name: idx_code_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_code_tenant_id_environment_id ON public.tax_rates USING btree (tenant_id, environment_id, code) WHERE ((code IS NOT NULL) AND ((code)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_costsheet_tenant_environment_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_costsheet_tenant_environment_lookup_key ON public.costsheets USING btree (tenant_id, environment_id, lookup_key) WHERE (((status)::text = 'published'::text) AND (lookup_key IS NOT NULL) AND ((lookup_key)::text <> ''::text));


--
-- Name: idx_coupon_tenant_environment_coupon_code_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_coupon_tenant_environment_coupon_code_unique ON public.coupons USING btree (tenant_id, environment_id, coupon_code) WHERE ((coupon_code IS NOT NULL) AND ((coupon_code)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_customer_metadata_gin; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_customer_metadata_gin ON public.customers USING gin (metadata);


--
-- Name: idx_customer_tenant_environment_email; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_customer_tenant_environment_email ON public.customers USING btree (tenant_id, environment_id, email) WHERE ((email IS NOT NULL) AND ((email)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_entity_integration_mapping_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_entity_integration_mapping_unique ON public.entity_integration_mappings USING btree (tenant_id, environment_id, entity_type, entity_id, provider_type) WHERE ((status)::text = 'published'::text);


--
-- Name: idx_entity_lookup_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entity_lookup_active ON public.tax_associations USING btree (tenant_id, environment_id, entity_type, entity_id, status);


--
-- Name: idx_entity_tax_association_lookup; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_entity_tax_association_lookup ON public.tax_applieds USING btree (tenant_id, environment_id, entity_type, entity_id);


--
-- Name: idx_entity_tax_rate_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_entity_tax_rate_id ON public.tax_applieds USING btree (tenant_id, environment_id, entity_type, entity_id, tax_rate_id);


--
-- Name: idx_environment_tenant_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_environment_tenant_created_at ON public.environments USING btree (tenant_id, created_at);


--
-- Name: idx_environment_tenant_id_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_environment_tenant_id_type ON public.environments USING btree (tenant_id, type);


--
-- Name: idx_environment_tenant_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_environment_tenant_status ON public.environments USING btree (tenant_id, status);


--
-- Name: idx_feature_tenant_env_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_created_at ON public.features USING btree (tenant_id, environment_id, created_at);


--
-- Name: idx_feature_tenant_env_group_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_group_id ON public.features USING btree (tenant_id, environment_id, group_id);


--
-- Name: idx_feature_tenant_env_lookup_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_feature_tenant_env_lookup_key_unique ON public.features USING btree (tenant_id, environment_id, lookup_key) WHERE ((lookup_key IS NOT NULL) AND ((lookup_key)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_feature_tenant_env_meter_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_meter_id ON public.features USING btree (tenant_id, environment_id, meter_id) WHERE (meter_id IS NOT NULL);


--
-- Name: idx_feature_tenant_env_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_status ON public.features USING btree (tenant_id, environment_id, status);


--
-- Name: idx_feature_tenant_env_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_feature_tenant_env_type ON public.features USING btree (tenant_id, environment_id, type);


--
-- Name: idx_gateway_attempt; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_gateway_attempt ON public.payment_attempts USING btree (gateway_attempt_id) WHERE (gateway_attempt_id IS NOT NULL);


--
-- Name: idx_group_tenant_environment_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_group_tenant_environment_lookup_key ON public.groups USING btree (tenant_id, environment_id, lookup_key) WHERE (((status)::text = 'published'::text) AND (lookup_key IS NOT NULL) AND ((lookup_key)::text <> ''::text));


--
-- Name: idx_incoming_webhook_events_request_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_incoming_webhook_events_request_id ON public.incoming_webhook_events USING btree (request_id);


--
-- Name: idx_incoming_webhook_events_tenant_env_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_incoming_webhook_events_tenant_env_created ON public.incoming_webhook_events USING btree (tenant_id, environment_id, created_at);


--
-- Name: idx_incoming_webhook_events_tenant_env_provider_created; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_incoming_webhook_events_tenant_env_provider_created ON public.incoming_webhook_events USING btree (tenant_id, environment_id, provider, created_at);


--
-- Name: idx_li_probe; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_li_probe ON public.subscription_line_items USING btree (tenant_id, environment_id, subscription_id, price_id) WHERE (((status)::text = 'published'::text) AND ((entity_type)::text = 'plan'::text));


--
-- Name: idx_meter_tenant_env_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_meter_tenant_env_status ON public.meters USING btree (tenant_id, environment_id, status) WHERE ((status)::text = ANY (ARRAY['published'::text, 'archived'::text]));


--
-- Name: idx_meters_active; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_meters_active ON public.meters USING btree (tenant_id, environment_id, event_name, created_at DESC) WHERE ((status)::text <> ALL ((ARRAY['deleted'::character varying, 'archived'::character varying])::text[]));


--
-- Name: idx_payment_attempt_number_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_payment_attempt_number_unique ON public.payment_attempts USING btree (payment_id, attempt_number);


--
-- Name: idx_payment_attempt_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_payment_attempt_status ON public.payment_attempts USING btree (payment_id, status);


--
-- Name: idx_plan_id_not_null; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plan_id_not_null ON public.credit_grants USING btree (tenant_id, environment_id, scope, plan_id) WHERE (plan_id IS NOT NULL);


--
-- Name: idx_price_plan_lookup_full; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_price_plan_lookup_full ON public.prices USING btree (tenant_id, environment_id, entity_type, entity_id, currency, billing_period, billing_period_count) WHERE (((status)::text = 'published'::text) AND ((entity_type)::text = 'PLAN'::text));


--
-- Name: idx_price_subscription_override; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_price_subscription_override ON public.prices USING btree (tenant_id, environment_id, entity_id, parent_price_id) WHERE (((status)::text = 'published'::text) AND ((entity_type)::text = 'SUBSCRIPTION'::text));


--
-- Name: idx_refund_gateway_refund_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_refund_gateway_refund_id ON public.refunds USING btree (gateway_refund_id);


--
-- Name: idx_refund_tenant_env_idempotency; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_refund_tenant_env_idempotency ON public.refunds USING btree (tenant_id, environment_id, idempotency_key);


--
-- Name: idx_refund_tenant_payment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_refund_tenant_payment ON public.refunds USING btree (tenant_id, environment_id, payment_id);


--
-- Name: idx_refund_tenant_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_refund_tenant_status ON public.refunds USING btree (tenant_id, environment_id, refund_status);


--
-- Name: idx_subscription_id_not_null; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_id_not_null ON public.credit_grants USING btree (tenant_id, environment_id, scope, subscription_id) WHERE (subscription_id IS NOT NULL);


--
-- Name: idx_subscription_line_items_sub_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_line_items_sub_id_status ON public.subscription_line_items USING btree (subscription_id, status);


--
-- Name: idx_subscription_period_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscription_period_unique ON public.invoices USING btree (subscription_id, period_start, period_end) WHERE (((invoice_status)::text <> 'VOIDED'::text) AND (subscription_id IS NOT NULL));


--
-- Name: idx_system_events_tenant_env; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_system_events_tenant_env ON public.system_events USING btree (tenant_id, environment_id);


--
-- Name: idx_tasks_tenant_env_task_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_tenant_env_task_status ON public.tasks USING btree (tenant_id, environment_id, task_status, status);


--
-- Name: idx_tasks_tenant_env_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_tenant_env_type_status ON public.tasks USING btree (tenant_id, environment_id, task_type, entity_type, status);


--
-- Name: idx_tasks_tenant_env_user; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tasks_tenant_env_user ON public.tasks USING btree (tenant_id, environment_id, created_by, status);


--
-- Name: idx_tax_rate_id_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tax_rate_id_tenant_id_environment_id ON public.tax_associations USING btree (tenant_id, environment_id, tax_rate_id);


--
-- Name: idx_tenant_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_created_at ON public.tenants USING btree (created_at);


--
-- Name: idx_tenant_customer_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_customer_status ON public.invoices USING btree (tenant_id, environment_id, customer_id, invoice_status, payment_status, status) WHERE ((status)::text = 'published'::text);


--
-- Name: idx_tenant_destination_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_destination_status ON public.payments USING btree (tenant_id, environment_id, destination_type, destination_id, payment_status, status);


--
-- Name: idx_tenant_due_date_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_due_date_status ON public.invoices USING btree (tenant_id, environment_id, due_date, invoice_status, payment_status, status);


--
-- Name: idx_tenant_environment_credit_note_number_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_credit_note_number_unique ON public.credit_notes USING btree (tenant_id, environment_id, credit_note_number) WHERE ((credit_note_number IS NOT NULL) AND ((credit_note_number)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_tenant_environment_creditnote_idempotency_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_creditnote_idempotency_key_unique ON public.credit_notes USING btree (tenant_id, environment_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL) AND ((idempotency_key)::text <> ''::text));


--
-- Name: idx_tenant_environment_external_id_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_external_id_unique ON public.customers USING btree (tenant_id, environment_id, external_id) WHERE ((external_id IS NOT NULL) AND ((external_id)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_tenant_environment_idempotency_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_idempotency_key ON public.wallet_transactions USING btree (tenant_id, environment_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL) AND ((idempotency_key)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_tenant_environment_idempotency_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_idempotency_key_unique ON public.invoices USING btree (tenant_id, environment_id, idempotency_key) WHERE ((idempotency_key IS NOT NULL) AND ((status)::text = 'published'::text) AND ((invoice_status)::text <> 'VOIDED'::text));


--
-- Name: idx_tenant_environment_invoice_number_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_invoice_number_unique ON public.invoices USING btree (tenant_id, environment_id, invoice_number) WHERE ((invoice_number IS NOT NULL) AND ((invoice_number)::text <> ''::text) AND ((status)::text = 'published'::text));


--
-- Name: idx_tenant_environment_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_lookup_key ON public.plans USING btree (tenant_id, environment_id, lookup_key) WHERE (((status)::text = 'published'::text) AND (lookup_key IS NOT NULL) AND ((lookup_key)::text <> ''::text));


--
-- Name: idx_tenant_environment_parent_transaction_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_environment_parent_transaction_status ON public.wallet_transactions USING btree (tenant_id, environment_id, parent_transaction_id, transaction_status);


--
-- Name: idx_tenant_environment_payment_idempotency_key_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_tenant_environment_payment_idempotency_key_unique ON public.payments USING btree (tenant_id, environment_id, idempotency_key);


--
-- Name: idx_tenant_gateway_payment; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_gateway_payment ON public.payments USING btree (tenant_id, environment_id, payment_gateway, gateway_payment_id) WHERE ((payment_gateway IS NOT NULL) AND (gateway_payment_id IS NOT NULL));


--
-- Name: idx_tenant_payment_method_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_payment_method_status ON public.payments USING btree (tenant_id, environment_id, payment_method_type, payment_method_id, payment_status, status);


--
-- Name: idx_tenant_subscription_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_subscription_status ON public.invoices USING btree (tenant_id, environment_id, subscription_id, invoice_status, payment_status, status);


--
-- Name: idx_tenant_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_type_status ON public.invoices USING btree (tenant_id, environment_id, invoice_type, invoice_status, payment_status, status);


--
-- Name: idx_tenant_wallet_type_credits_available_expiry_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_tenant_wallet_type_credits_available_expiry_date ON public.wallet_transactions USING btree (tenant_id, environment_id, wallet_id, type, credits_available, expiry_date) WHERE ((credits_available > (0)::numeric) AND ((type)::text = 'credit'::text));


--
-- Name: idx_user_email_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_user_email_unique ON public.users USING btree (email) WHERE (((status)::text = 'published'::text) AND (email IS NOT NULL) AND ((email)::text <> ''::text));


--
-- Name: idx_user_tenant_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_tenant_created_at ON public.users USING btree (tenant_id, created_at);


--
-- Name: idx_user_tenant_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_user_tenant_status ON public.users USING btree (tenant_id, status, type);


--
-- Name: idx_workflow_executions_start_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflow_executions_start_time ON public.workflow_executions USING btree (start_time);


--
-- Name: idx_workflow_executions_tenant_env_entity; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflow_executions_tenant_env_entity ON public.workflow_executions USING btree (tenant_id, environment_id, entity, entity_id);


--
-- Name: idx_workflow_executions_tenant_env_queue; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflow_executions_tenant_env_queue ON public.workflow_executions USING btree (tenant_id, environment_id, task_queue);


--
-- Name: idx_workflow_executions_tenant_env_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflow_executions_tenant_env_status ON public.workflow_executions USING btree (tenant_id, environment_id, workflow_status);


--
-- Name: idx_workflow_executions_tenant_env_time; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflow_executions_tenant_env_time ON public.workflow_executions USING btree (tenant_id, environment_id, start_time);


--
-- Name: idx_workflow_executions_tenant_env_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_workflow_executions_tenant_env_type ON public.workflow_executions USING btree (tenant_id, environment_id, workflow_type);


--
-- Name: idx_workflow_executions_workflow_run_unique; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_workflow_executions_workflow_run_unique ON public.workflow_executions USING btree (workflow_id, run_id);


--
-- Name: invoice_line_items_subscription_line_item_id_idx; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoice_line_items_subscription_line_item_id_idx ON public.invoice_line_items USING btree (tenant_id, environment_id, subscription_line_item_id, status) WHERE (subscription_line_item_id IS NOT NULL);


--
-- Name: invoicelineitem_period_start_period_end; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_period_start_period_end ON public.invoice_line_items USING btree (period_start, period_end);


--
-- Name: invoicelineitem_subscription_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_subscription_id_status ON public.invoice_line_items USING btree (subscription_id, status);


--
-- Name: invoicelineitem_tenant_id_environment_id_customer_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_environment_id_customer_id_status ON public.invoice_line_items USING btree (tenant_id, environment_id, customer_id, status);


--
-- Name: invoicelineitem_tenant_id_environment_id_invoice_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_environment_id_invoice_id_status ON public.invoice_line_items USING btree (tenant_id, environment_id, invoice_id, status);


--
-- Name: invoicelineitem_tenant_id_environment_id_meter_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_environment_id_meter_id_status ON public.invoice_line_items USING btree (tenant_id, environment_id, meter_id, status);


--
-- Name: invoicelineitem_tenant_id_environment_id_price_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_environment_id_price_id_status ON public.invoice_line_items USING btree (tenant_id, environment_id, price_id, status);


--
-- Name: invoicelineitem_tenant_id_environment_id_subscription_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_environment_id_subscription_id_status ON public.invoice_line_items USING btree (tenant_id, environment_id, subscription_id, status);


--
-- Name: invoicesequence_tenant_id_environment_id_year_month; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX invoicesequence_tenant_id_environment_id_year_month ON public.invoice_sequences USING btree (tenant_id, environment_id, year_month);


--
-- Name: meter_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX meter_tenant_id_environment_id ON public.meters USING btree (tenant_id, environment_id);


--
-- Name: paymentmethod_tenant_id_environment_id_customer_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX paymentmethod_tenant_id_environment_id_customer_id_status ON public.payment_methods USING btree (tenant_id, environment_id, customer_id, status) WHERE ((status)::text = 'published'::text);


--
-- Name: plan_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX plan_tenant_id_environment_id ON public.plans USING btree (tenant_id, environment_id);


--
-- Name: price_start_date_end_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX price_start_date_end_date ON public.prices USING btree (start_date, end_date);


--
-- Name: price_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX price_tenant_id_environment_id ON public.prices USING btree (tenant_id, environment_id);


--
-- Name: price_tenant_id_environment_id_entity_id_entity_type_sequence; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX price_tenant_id_environment_id_entity_id_entity_type_sequence ON public.prices USING btree (tenant_id, environment_id, entity_id, entity_type, sequence) WHERE ((status)::text = 'published'::text);


--
-- Name: price_tenant_id_environment_id_entity_id_parent_price_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX price_tenant_id_environment_id_entity_id_parent_price_id ON public.prices USING btree (tenant_id, environment_id, entity_id, parent_price_id) WHERE (((status)::text = 'published'::text) AND ((entity_type)::text = 'SUBSCRIPTION'::text));


--
-- Name: price_tenant_id_environment_id_group_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX price_tenant_id_environment_id_group_id ON public.prices USING btree (tenant_id, environment_id, group_id);


--
-- Name: price_tenant_id_environment_id_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX price_tenant_id_environment_id_lookup_key ON public.prices USING btree (tenant_id, environment_id, lookup_key) WHERE (((status)::text = 'published'::text) AND (lookup_key IS NOT NULL) AND ((lookup_key)::text <> ''::text) AND (end_date IS NULL));


--
-- Name: price_tenant_id_environment_id_plan_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX price_tenant_id_environment_id_plan_id ON public.prices USING btree (tenant_id, environment_id, plan_id);


--
-- Name: priceunit_code_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX priceunit_code_tenant_id_environment_id ON public.price_unit USING btree (code, tenant_id, environment_id) WHERE ((status)::text = 'published'::text);


--
-- Name: priceunit_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX priceunit_tenant_id_environment_id ON public.price_unit USING btree (tenant_id, environment_id);


--
-- Name: priceunit_tenant_id_environment_id_code; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX priceunit_tenant_id_environment_id_code ON public.price_units USING btree (tenant_id, environment_id, code) WHERE ((status)::text = 'published'::text);


--
-- Name: scheduledtask_connection_id_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX scheduledtask_connection_id_enabled ON public.scheduled_tasks USING btree (connection_id, enabled);


--
-- Name: scheduledtask_connection_id_entity_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX scheduledtask_connection_id_entity_type_status ON public.scheduled_tasks USING btree (connection_id, entity_type, status);


--
-- Name: scheduledtask_entity_type_interval_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX scheduledtask_entity_type_interval_enabled ON public.scheduled_tasks USING btree (entity_type, "interval", enabled);


--
-- Name: scheduledtask_tenant_id_environment_id_enabled; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX scheduledtask_tenant_id_environment_id_enabled ON public.scheduled_tasks USING btree (tenant_id, environment_id, enabled);


--
-- Name: secret_tenant_id_environment_id_provider_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_tenant_id_environment_id_provider_status ON public.secrets USING btree (tenant_id, environment_id, provider, status);


--
-- Name: secret_tenant_id_environment_id_type_provider_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_tenant_id_environment_id_type_provider_status ON public.secrets USING btree (tenant_id, environment_id, type, provider, status);


--
-- Name: secret_tenant_id_environment_id_type_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_tenant_id_environment_id_type_status ON public.secrets USING btree (tenant_id, environment_id, type, status);


--
-- Name: secret_type_value_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX secret_type_value_status ON public.secrets USING btree (type, value, status);


--
-- Name: settings_tenant_id_environment_id_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX settings_tenant_id_environment_id_key ON public.settings USING btree (tenant_id, environment_id, key) WHERE ((status)::text = 'published'::text);


--
-- Name: settings_tenant_id_environment_id_status_key; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX settings_tenant_id_environment_id_status_key ON public.settings USING btree (tenant_id, environment_id, status, key);


--
-- Name: subscription_tenant_id_environ_c25981d50dd395970a0aa50e37ab6860; Type: INDEX; Schema: public; Owner: -
--
-- RENAMED FROM PROD: prod calls this index subscription_plan_synced_price_sequence_id_idx.
-- main's Ent schema (ba513ce52) expects the hashed name below; Ent matches
-- indexes by name only, so the baseline uses Ent's name. Same columns, same
-- predicate: this is a rename, not new coverage.
--


CREATE INDEX subscription_tenant_id_environ_c25981d50dd395970a0aa50e37ab6860 ON public.subscriptions USING btree (tenant_id, environment_id, plan_id, synced_price_sequence, id) WHERE (((status)::text = 'published'::text) AND ((subscription_type)::text = ANY (ARRAY[('standalone'::character varying)::text, ('delegated_invoicing'::character varying)::text, ('parent'::character varying)::text, ('grouped_invoicing'::character varying)::text])));


--
-- Name: subscription_tenant_id_environ_20e3b0b72267ce9c02ece1d822473bbb; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environ_20e3b0b72267ce9c02ece1d822473bbb ON public.subscriptions USING btree (tenant_id, environment_id, current_period_end, subscription_status, status);


--
-- Name: subscription_tenant_id_environ_567bef0e84d52ac70316e43cacd7ae6b; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environ_567bef0e84d52ac70316e43cacd7ae6b ON public.subscriptions USING btree (tenant_id, environment_id, subscription_status, status);


--
-- Name: subscription_tenant_id_environ_75161f6f0dc30cd367e8deea7dd3064d; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environ_75161f6f0dc30cd367e8deea7dd3064d ON public.subscriptions USING btree (tenant_id, environment_id, subscription_status, collection_method, status) WHERE ((subscription_status)::text = ANY (ARRAY[('incomplete'::character varying)::text, ('past_due'::character varying)::text]));


--
-- Name: subscription_tenant_id_environment_id_active_pause_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environment_id_active_pause_id_status ON public.subscriptions USING btree (tenant_id, environment_id, active_pause_id, status);


--
-- Name: subscription_tenant_id_environment_id_collection_method_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environment_id_collection_method_status ON public.subscriptions USING btree (tenant_id, environment_id, collection_method, status);


--
-- Name: subscription_tenant_id_environment_id_customer_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environment_id_customer_id_status ON public.subscriptions USING btree (tenant_id, environment_id, customer_id, status) WHERE ((status)::text = 'published'::text);


--
-- Name: subscription_tenant_id_environment_id_pause_status_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environment_id_pause_status_status ON public.subscriptions USING btree (tenant_id, environment_id, pause_status, status);


--
-- Name: subscription_tenant_id_environment_id_payment_behavior_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environment_id_payment_behavior_status ON public.subscriptions USING btree (tenant_id, environment_id, payment_behavior, status);


--
-- Name: subscription_tenant_id_environment_id_plan_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_environment_id_plan_id_status ON public.subscriptions USING btree (tenant_id, environment_id, plan_id, status);


--
-- Name: subscriptionlineitem_start_date_end_date; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_start_date_end_date ON public.subscription_line_items USING btree (start_date, end_date);


--
-- Name: subscriptionlineitem_subscription_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_subscription_id_status ON public.subscription_line_items USING btree (subscription_id, status);


--
-- Name: subscriptionlineitem_tenant_id_46d81ab8b452b2ec243538fba6bf18c8; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_tenant_id_46d81ab8b452b2ec243538fba6bf18c8 ON public.subscription_line_items USING btree (tenant_id, environment_id, customer_id, status);


--
-- Name: subscriptionlineitem_tenant_id_93a70bcba6ddbf090741a3546f946a38; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_tenant_id_93a70bcba6ddbf090741a3546f946a38 ON public.subscription_line_items USING btree (tenant_id, environment_id, subscription_id, status);


--
-- Name: subscriptionlineitem_tenant_id_c6fd8ad171373b4dc97f862d8396c125; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_tenant_id_c6fd8ad171373b4dc97f862d8396c125 ON public.subscription_line_items USING btree (tenant_id, environment_id, entity_id, entity_type, status);


--
-- Name: subscriptionlineitem_tenant_id_environment_id_meter_id_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_tenant_id_environment_id_meter_id_customer ON public.subscription_line_items USING btree (tenant_id, environment_id, meter_id, customer_id, status);


--
-- Name: subscriptionlineitem_tenant_id_environment_id_meter_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_tenant_id_environment_id_meter_id_status ON public.subscription_line_items USING btree (tenant_id, environment_id, meter_id, status);


--
-- Name: subscriptionlineitem_tenant_id_environment_id_plan_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_tenant_id_environment_id_plan_id_status ON public.subscription_line_items USING btree (tenant_id, environment_id, plan_id, status);


--
-- Name: subscriptionlineitem_tenant_id_environment_id_price_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionlineitem_tenant_id_environment_id_price_id_status ON public.subscription_line_items USING btree (tenant_id, environment_id, price_id, status);


--
-- Name: subscriptionlineitem_tenant_id_f0ae9089e4afd8b35c340e4fa52379c2; Type: INDEX; Schema: public; Owner: -
--
-- RENAMED FROM PROD: prod calls this index subscriptionlineitem_tenant_id_environment_id_subscription_id_p.
-- main's Ent schema (ba513ce52) expects the hashed name below; Ent matches
-- indexes by name only, so the baseline uses Ent's name. Same columns, same
-- predicate: this is a rename, not new coverage.
--

CREATE INDEX subscriptionlineitem_tenant_id_f0ae9089e4afd8b35c340e4fa52379c2 ON public.subscription_line_items USING btree (tenant_id, environment_id, subscription_id, price_id, entity_type) WHERE ((status)::text = 'published'::text);


--
-- Name: subscriptionpause_tenant_id_en_010ac21c0eb5173551bb9439e21dbbf0; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionpause_tenant_id_en_010ac21c0eb5173551bb9439e21dbbf0 ON public.subscription_pauses USING btree (tenant_id, environment_id, subscription_id, status);


--
-- Name: subscriptionpause_tenant_id_environment_id_pause_end_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionpause_tenant_id_environment_id_pause_end_status ON public.subscription_pauses USING btree (tenant_id, environment_id, pause_end, status);


--
-- Name: subscriptionpause_tenant_id_environment_id_pause_start_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionpause_tenant_id_environment_id_pause_start_status ON public.subscription_pauses USING btree (tenant_id, environment_id, pause_start, status);


--
-- Name: subscriptionphase_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionphase_tenant_id_environment_id ON public.subscription_phases USING btree (tenant_id, environment_id);


--
-- Name: subscriptionschedule_scheduled_at_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionschedule_scheduled_at_status ON public.subscription_schedules USING btree (scheduled_at, status) WHERE ((status)::text = 'pending'::text);


--
-- Name: subscriptionschedule_status_schedule_type; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionschedule_status_schedule_type ON public.subscription_schedules USING btree (status, schedule_type) WHERE ((status)::text = 'pending'::text);


--
-- Name: subscriptionschedule_subscription_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionschedule_subscription_id ON public.subscription_schedules USING btree (subscription_id);


--
-- Name: subscriptionschedule_subscription_id_schedule_type; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX subscriptionschedule_subscription_id_schedule_type ON public.subscription_schedules USING btree (subscription_id, schedule_type) WHERE ((status)::text = 'pending'::text);


--
-- Name: subscriptionschedule_tenant_id_environment_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscriptionschedule_tenant_id_environment_id ON public.subscription_schedules USING btree (tenant_id, environment_id);


--
-- Name: unique_entity_tax_mapping; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX unique_entity_tax_mapping ON public.tax_associations USING btree (tenant_id, environment_id, entity_type, entity_id, tax_rate_id) WHERE ((status)::text = 'published'::text);


--
-- Name: usagerecord_tenant_id_environm_38da5f8421b6cb22df743efecaf2b39d; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX usagerecord_tenant_id_environm_38da5f8421b6cb22df743efecaf2b39d ON public.usage_records USING btree (tenant_id, environment_id, subscription_id, period_start, period_end) WHERE ((status)::text = 'published'::text);


--
-- Name: usagerecord_tenant_id_environment_id_synced; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX usagerecord_tenant_id_environment_id_synced ON public.usage_records USING btree (tenant_id, environment_id, synced);


--
-- Name: wallet_tenant_id_environment_id_customer_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallet_tenant_id_environment_id_customer_id_status ON public.wallets USING btree (tenant_id, environment_id, customer_id, status);


--
-- Name: wallet_tenant_id_environment_id_status_wallet_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallet_tenant_id_environment_id_status_wallet_status ON public.wallets USING btree (tenant_id, environment_id, status, wallet_status);


--
-- Name: wallettransaction_tenant_id_en_42be755be82746b466cc41b854e85b8c; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallettransaction_tenant_id_en_42be755be82746b466cc41b854e85b8c ON public.wallet_transactions USING btree (tenant_id, environment_id, reference_type, reference_id, status);


--
-- Name: wallettransaction_tenant_id_environment_id_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallettransaction_tenant_id_environment_id_created_at ON public.wallet_transactions USING btree (tenant_id, environment_id, created_at);


--
-- Name: wallettransaction_tenant_id_environment_id_customer_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallettransaction_tenant_id_environment_id_customer_id ON public.wallet_transactions USING btree (tenant_id, environment_id, customer_id);


--
-- Name: wallettransaction_tenant_id_environment_id_wallet_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallettransaction_tenant_id_environment_id_wallet_id ON public.wallet_transactions USING btree (tenant_id, environment_id, wallet_id);


--
-- Name: costsheets costsheets_prices_costsheet; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.costsheets
    ADD CONSTRAINT costsheets_prices_costsheet FOREIGN KEY (price_costsheet) REFERENCES public.prices(id) ON DELETE SET NULL;


--
-- Name: coupon_applications coupon_applications_coupons_coupon_applications; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_applications
    ADD CONSTRAINT coupon_applications_coupons_coupon_applications FOREIGN KEY (coupon_id) REFERENCES public.coupons(id);


--
-- Name: coupon_applications coupon_applications_invoice_line_items_coupon_applications; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_applications
    ADD CONSTRAINT coupon_applications_invoice_line_items_coupon_applications FOREIGN KEY (invoice_line_item_id) REFERENCES public.invoice_line_items(id) ON DELETE SET NULL;


--
-- Name: coupon_applications coupon_applications_invoices_coupon_applications; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_applications
    ADD CONSTRAINT coupon_applications_invoices_coupon_applications FOREIGN KEY (invoice_id) REFERENCES public.invoices(id);


--
-- Name: coupon_applications coupon_applications_subscriptions_coupon_applications; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_applications
    ADD CONSTRAINT coupon_applications_subscriptions_coupon_applications FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id) ON DELETE SET NULL;


--
-- Name: coupon_association_coupon_applications coupon_association_coupon_applications_coupon_application_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_association_coupon_applications
    ADD CONSTRAINT coupon_association_coupon_applications_coupon_application_id FOREIGN KEY (coupon_application_id) REFERENCES public.coupon_applications(id) ON DELETE CASCADE;


--
-- Name: coupon_association_coupon_applications coupon_association_coupon_applications_coupon_association_id; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_association_coupon_applications
    ADD CONSTRAINT coupon_association_coupon_applications_coupon_association_id FOREIGN KEY (coupon_association_id) REFERENCES public.coupon_associations(id) ON DELETE CASCADE;


--
-- Name: coupon_associations coupon_associations_coupons_coupon_associations; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_associations
    ADD CONSTRAINT coupon_associations_coupons_coupon_associations FOREIGN KEY (coupon_id) REFERENCES public.coupons(id);


--
-- Name: coupon_associations coupon_associations_subscription_line_items_coupon_associations; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_associations
    ADD CONSTRAINT coupon_associations_subscription_line_items_coupon_associations FOREIGN KEY (subscription_line_item_id) REFERENCES public.subscription_line_items(id) ON DELETE SET NULL;


--
-- Name: coupon_associations coupon_associations_subscriptions_coupon_associations; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.coupon_associations
    ADD CONSTRAINT coupon_associations_subscriptions_coupon_associations FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id);


--
-- Name: credit_grants credit_grants_addons_credit_grants; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_grants
    ADD CONSTRAINT credit_grants_addons_credit_grants FOREIGN KEY (addon_id) REFERENCES public.addons(id) ON DELETE SET NULL;


--
-- Name: credit_grants credit_grants_plans_credit_grants; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_grants
    ADD CONSTRAINT credit_grants_plans_credit_grants FOREIGN KEY (plan_id) REFERENCES public.plans(id) ON DELETE SET NULL;


--
-- Name: credit_grants credit_grants_subscriptions_credit_grants; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_grants
    ADD CONSTRAINT credit_grants_subscriptions_credit_grants FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id) ON DELETE SET NULL;


--
-- Name: credit_note_line_items credit_note_line_items_credit_notes_line_items; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.credit_note_line_items
    ADD CONSTRAINT credit_note_line_items_credit_notes_line_items FOREIGN KEY (credit_note_id) REFERENCES public.credit_notes(id);


--
-- Name: entitlements entitlements_addons_entitlements; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.entitlements
    ADD CONSTRAINT entitlements_addons_entitlements FOREIGN KEY (addon_entitlements) REFERENCES public.addons(id) ON DELETE SET NULL;


--
-- Name: invoice_line_items invoice_line_items_invoices_line_items; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoice_line_items
    ADD CONSTRAINT invoice_line_items_invoices_line_items FOREIGN KEY (invoice_id) REFERENCES public.invoices(id);


--
-- Name: payment_attempts payment_attempts_payments_attempts; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.payment_attempts
    ADD CONSTRAINT payment_attempts_payments_attempts FOREIGN KEY (payment_id) REFERENCES public.payments(id);


--
-- Name: prices prices_price_units_price_unit_edge; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.prices
    ADD CONSTRAINT prices_price_units_price_unit_edge FOREIGN KEY (price_unit_id) REFERENCES public.price_units(id) ON DELETE SET NULL;


--
-- Name: subscription_line_items subscription_line_items_subscriptions_line_items; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_line_items
    ADD CONSTRAINT subscription_line_items_subscriptions_line_items FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id);


--
-- Name: subscription_pauses subscription_pauses_subscriptions_pauses; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_pauses
    ADD CONSTRAINT subscription_pauses_subscriptions_pauses FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id);


--
-- Name: subscription_phases subscription_phases_subscriptions_phases; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_phases
    ADD CONSTRAINT subscription_phases_subscriptions_phases FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id);


--
-- Name: subscription_schedules subscription_schedules_subscriptions_schedules; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscription_schedules
    ADD CONSTRAINT subscription_schedules_subscriptions_schedules FOREIGN KEY (subscription_id) REFERENCES public.subscriptions(id);


--
-- Name: subscriptions subscriptions_customers_invoicing_customer; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.subscriptions
    ADD CONSTRAINT subscriptions_customers_invoicing_customer FOREIGN KEY (invoicing_customer_id) REFERENCES public.customers(id) ON DELETE SET NULL;


--
-- PostgreSQL database dump complete
--


