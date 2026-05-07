--
-- PostgreSQL bootstrap: schemas and extensions only.
--
-- All entity tables are created by the Ent ORM migration layer (make migrate-ent).
-- This file only sets up the pre-requisites that must exist before Ent can run.
--

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

-- ── Schemas ───────────────────────────────────────────────────────────────────

CREATE SCHEMA IF NOT EXISTS public;
COMMENT ON SCHEMA public IS 'standard public schema';

-- extensions schema is used to isolate uuid-ossp from the public namespace
CREATE SCHEMA IF NOT EXISTS extensions;

-- ── Extensions ────────────────────────────────────────────────────────────────

CREATE EXTENSION IF NOT EXISTS "uuid-ossp" SCHEMA extensions;
