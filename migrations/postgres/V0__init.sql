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

CREATE SCHEMA public;


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
    provider character varying NOT NULL,
    token character varying NOT NULL,
    status character varying DEFAULT 'published'::character varying NOT NULL,
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
    email character varying(255) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying(255) NOT NULL,
    updated_by character varying(255) NOT NULL
);


--
-- Name: environments; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.environments (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    name character varying(255) NOT NULL,
    type character varying(50) NOT NULL,
    slug character varying(255) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying(255) NOT NULL,
    updated_by character varying(255) NOT NULL
);


--
-- Name: invoice_line_items; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoice_line_items (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    customer_id character varying(50) NOT NULL,
    subscription_id character varying(50),
    price_id character varying(50) NOT NULL,
    meter_id character varying(50),
    amount numeric(20,8) NOT NULL,
    quantity numeric(20,8) NOT NULL,
    currency character varying(10) NOT NULL,
    period_start timestamp with time zone,
    period_end timestamp with time zone,
    metadata jsonb,
    invoice_id character varying(50) NOT NULL
);


CREATE TABLE public.invoice_line_items (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    customer_id character varying(50) NOT NULL,
    subscription_id character varying(50),
    price_id character varying(50) NOT NULL,
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
    price_type character varying,
    meter_display_name character varying,
    display_name character varying
);

--
-- Name: invoices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.invoices (
    id character varying(50) NOT NULL,
    tenant_id character varying(50) NOT NULL,
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
    period_end timestamp with time zone
);


--
-- Name: meters; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.meters (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    tenant_id character varying(50) NOT NULL,
    event_name character varying(255) NOT NULL,
    aggregation jsonb NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying(255) NOT NULL,
    updated_by character varying(255) NOT NULL,
    filters jsonb DEFAULT '[]'::jsonb NOT NULL,
    reset_usage character varying(20) DEFAULT 'BILLING_PERIOD'::character varying NOT NULL,
    name character varying(255)
);


--
-- Name: plans; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.plans (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    lookup_key character varying(255) NOT NULL,
    name character varying(255) NOT NULL,
    description text,
    invoice_cadence character varying(20) NOT NULL,
    trial_period integer NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying(255) NOT NULL,
    updated_by character varying(255) NOT NULL
);


--
-- Name: prices; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.prices (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    amount numeric(25,15) NOT NULL,
    currency character varying(3) NOT NULL,
    display_amount character varying(255) NOT NULL,
    plan_id character varying(50) NOT NULL,
    type character varying(20) NOT NULL,
    billing_period character varying(20) NOT NULL,
    billing_period_count integer NOT NULL,
    billing_model character varying(20) NOT NULL,
    billing_cadence character varying(20) NOT NULL,
    meter_id character varying(50),
    filter_values jsonb,
    tier_mode character varying(20),
    tiers jsonb,
    transform_quantity jsonb,
    lookup_key character varying(255) NOT NULL,
    description text,
    metadata jsonb,
    tenant_id character varying(50) NOT NULL,
    status character varying(20) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying(255) NOT NULL,
    updated_by character varying(255) NOT NULL
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
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
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
    invoice_cadence character varying NOT NULL,
    billing_cadence character varying NOT NULL,
    billing_period character varying NOT NULL,
    billing_period_count bigint DEFAULT 1 NOT NULL,
    tenant_id character varying(50) NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    version bigint DEFAULT 1 NOT NULL
);


--
-- Name: tenants; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.tenants (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    name character varying(255) NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL
);


--
-- Name: users; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.users (
    id character varying(50) DEFAULT extensions.uuid_generate_v4() NOT NULL,
    email character varying NOT NULL,
    tenant_id character varying(50) NOT NULL,
    status character varying DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at timestamp with time zone DEFAULT CURRENT_TIMESTAMP NOT NULL,
    created_by character varying NOT NULL,
    updated_by character varying NOT NULL
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
    balance_before numeric(20,9) NOT NULL,
    balance_after numeric(20,9) NOT NULL,
    transaction_status character varying(50) DEFAULT 'pending'::character varying NOT NULL,
    reference_type character varying(50),
    reference_id character varying,
    description character varying,
    metadata jsonb,
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    transaction_type character varying DEFAULT 'credit'::character varying NOT NULL
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
    status character varying(50) DEFAULT 'published'::character varying NOT NULL,
    created_at timestamp with time zone NOT NULL,
    updated_at timestamp with time zone NOT NULL,
    created_by character varying,
    updated_by character varying,
    description character varying,
    name character varying
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
-- Name: idx_customers_external_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_customers_external_id ON public.customers USING btree (external_id);


--
-- Name: idx_customers_tenant_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_customers_tenant_id ON public.customers USING btree (tenant_id);


--
-- Name: idx_plans_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plans_lookup_key ON public.plans USING btree (lookup_key);


--
-- Name: idx_plans_tenant_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_plans_tenant_id ON public.plans USING btree (tenant_id);


--
-- Name: idx_prices_lookup_key; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prices_lookup_key ON public.prices USING btree (lookup_key);


--
-- Name: idx_prices_plan_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prices_plan_id ON public.prices USING btree (plan_id);


--
-- Name: idx_prices_tenant_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_prices_tenant_id ON public.prices USING btree (tenant_id);


--
-- Name: idx_subscriptions_customer_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_customer_id ON public.subscriptions USING btree (customer_id);


--
-- Name: idx_subscriptions_plan_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_plan_id ON public.subscriptions USING btree (plan_id);


--
-- Name: idx_subscriptions_tenant_id; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_subscriptions_tenant_id ON public.subscriptions USING btree (tenant_id);


--
-- Name: idx_transaction_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_transaction_created_at ON public.wallet_transactions USING btree (created_at DESC);


--
-- Name: idx_transaction_reference; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_transaction_reference ON public.wallet_transactions USING btree (reference_type, reference_id);


--
-- Name: idx_transaction_tenant_wallet; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_transaction_tenant_wallet ON public.wallet_transactions USING btree (tenant_id, wallet_id);


--
-- Name: idx_transaction_wallet_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_transaction_wallet_status ON public.wallet_transactions USING btree (wallet_id, transaction_status);


--
-- Name: idx_wallet_customer_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallet_customer_status ON public.wallets USING btree (customer_id, status);


--
-- Name: idx_wallet_tenant_customer; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallet_tenant_customer ON public.wallets USING btree (tenant_id, customer_id);


--
-- Name: idx_wallet_tenant_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_wallet_tenant_status ON public.wallets USING btree (tenant_id, status);


--
-- Name: invoice_period_start_period_end; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoice_period_start_period_end ON public.invoices USING btree (period_start, period_end);


--
-- Name: invoice_tenant_id_customer_id__5436a8fadd7b3d00f9cf02d1f518b1e6; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoice_tenant_id_customer_id__5436a8fadd7b3d00f9cf02d1f518b1e6 ON public.invoices USING btree (tenant_id, customer_id, invoice_status, payment_status, status);


--
-- Name: invoice_tenant_id_due_date_invoice_status_payment_status_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoice_tenant_id_due_date_invoice_status_payment_status_status ON public.invoices USING btree (tenant_id, due_date, invoice_status, payment_status, status);


--
-- Name: invoice_tenant_id_invoice_type_0c0c6a700e8ddb1cb450d2b191bc2607; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoice_tenant_id_invoice_type_0c0c6a700e8ddb1cb450d2b191bc2607 ON public.invoices USING btree (tenant_id, invoice_type, invoice_status, payment_status, status);


--
-- Name: invoice_tenant_id_subscription_14c8349bf0cdbe6ed5de20002c4dbed1; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoice_tenant_id_subscription_14c8349bf0cdbe6ed5de20002c4dbed1 ON public.invoices USING btree (tenant_id, subscription_id, invoice_status, payment_status, status);


--
-- Name: invoicelineitem_period_start_period_end; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_period_start_period_end ON public.invoice_line_items USING btree (period_start, period_end);


--
-- Name: invoicelineitem_tenant_id_customer_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_customer_id_status ON public.invoice_line_items USING btree (tenant_id, customer_id, status);


--
-- Name: invoicelineitem_tenant_id_invoice_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_invoice_id_status ON public.invoice_line_items USING btree (tenant_id, invoice_id, status);


--
-- Name: invoicelineitem_tenant_id_meter_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_meter_id_status ON public.invoice_line_items USING btree (tenant_id, meter_id, status);


--
-- Name: invoicelineitem_tenant_id_price_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_price_id_status ON public.invoice_line_items USING btree (tenant_id, price_id, status);


--
-- Name: invoicelineitem_tenant_id_subscription_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX invoicelineitem_tenant_id_subscription_id_status ON public.invoice_line_items USING btree (tenant_id, subscription_id, status);


--
-- Name: subscription_tenant_id_current_47b9be9b268282e0fecf31429a17b564; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_current_47b9be9b268282e0fecf31429a17b564 ON public.subscriptions USING btree (tenant_id, current_period_end, subscription_status, status);


--
-- Name: subscription_tenant_id_customer_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_customer_id_status ON public.subscriptions USING btree (tenant_id, customer_id, status);


--
-- Name: subscription_tenant_id_plan_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_plan_id_status ON public.subscriptions USING btree (tenant_id, plan_id, status);


--
-- Name: subscription_tenant_id_subscription_status_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX subscription_tenant_id_subscription_status_status ON public.subscriptions USING btree (tenant_id, subscription_status, status);


--
-- Name: wallet_tenant_id_customer_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallet_tenant_id_customer_id_status ON public.wallets USING btree (tenant_id, customer_id, status);


--
-- Name: wallet_tenant_id_status_wallet_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallet_tenant_id_status_wallet_status ON public.wallets USING btree (tenant_id, status, wallet_status);


--
-- Name: wallettransaction_created_at; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallettransaction_created_at ON public.wallet_transactions USING btree (created_at);


--
-- Name: wallettransaction_tenant_id_reference_type_reference_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallettransaction_tenant_id_reference_type_reference_id_status ON public.wallet_transactions USING btree (tenant_id, reference_type, reference_id, status);


--
-- Name: wallettransaction_tenant_id_wallet_id_status; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX wallettransaction_tenant_id_wallet_id_status ON public.wallet_transactions USING btree (tenant_id, wallet_id, status);


--
-- Name: invoice_line_items invoice_line_items_invoices_line_items; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.invoice_line_items
    ADD CONSTRAINT invoice_line_items_invoices_line_items FOREIGN KEY (invoice_id) REFERENCES public.invoices(id);


--
-- PostgreSQL database dump complete
--

