-- Owner-authorized testing setup only. Run with psql -X -v ON_ERROR_STOP=1.
-- The invoking tool must verify TLS/authentication and refuse existing targets.
-- Passwords are supplied through environment, never committed SQL or CLI args.
\getenv dev_app_password BIHO_DEV_APP_PASSWORD
\getenv dev_migration_password BIHO_DEV_MIGRATION_PASSWORD
\getenv test_app_password BIHO_TEST_APP_PASSWORD
\getenv test_migration_password BIHO_TEST_MIGRATION_PASSWORD
BEGIN;
CREATE ROLE biho_live_dev_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 2 PASSWORD :'dev_migration_password';
CREATE ROLE biho_live_dev_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 12 PASSWORD :'dev_app_password';
CREATE ROLE biho_live_test_migrator LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 2 PASSWORD :'test_migration_password';
CREATE ROLE biho_live_test_app LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOREPLICATION CONNECTION LIMIT 12 PASSWORD :'test_app_password';
COMMIT;
CREATE DATABASE biho_live_dev OWNER biho_live_dev_migrator TEMPLATE template0;
CREATE DATABASE biho_live_test OWNER biho_live_test_migrator TEMPLATE template0;
REVOKE ALL ON DATABASE biho_live_dev FROM PUBLIC;
REVOKE ALL ON DATABASE biho_live_test FROM PUBLIC;
GRANT CONNECT ON DATABASE biho_live_dev TO biho_live_dev_app;
GRANT CONNECT ON DATABASE biho_live_test TO biho_live_test_app;
\connect biho_live_dev
REVOKE ALL ON SCHEMA public FROM PUBLIC;
CREATE SCHEMA migration AUTHORIZATION biho_live_dev_migrator;
ALTER DEFAULT PRIVILEGES FOR ROLE biho_live_dev_migrator GRANT USAGE ON SCHEMAS TO biho_live_dev_app;
ALTER DEFAULT PRIVILEGES FOR ROLE biho_live_dev_migrator IN SCHEMA migration REVOKE ALL ON TABLES FROM biho_live_dev_app;
\connect biho_live_test
REVOKE ALL ON SCHEMA public FROM PUBLIC;
CREATE SCHEMA migration AUTHORIZATION biho_live_test_migrator;
ALTER DEFAULT PRIVILEGES FOR ROLE biho_live_test_migrator GRANT USAGE ON SCHEMAS TO biho_live_test_app;
ALTER DEFAULT PRIVILEGES FOR ROLE biho_live_test_migrator IN SCHEMA migration REVOKE ALL ON TABLES FROM biho_live_test_app;
