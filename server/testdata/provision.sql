-- Synthetic fixture only. Run in a fresh disposable PostgreSQL service.
CREATE ROLE portal_migrator LOGIN PASSWORD 'fixture-only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
CREATE ROLE portal_runtime LOGIN PASSWORD 'fixture-only' NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOBYPASSRLS;
CREATE DATABASE portal_identity_test OWNER portal_migrator;
\connect portal_identity_test
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO portal_runtime;
