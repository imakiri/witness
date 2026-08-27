-- Reverse of schema.up.sql. Drops the witness schema and all of its tables,
-- indexes and views. Leaves the pg_trgm extension in place because it is a
-- database-global resource that other schemas may rely on.

DROP SCHEMA witness CASCADE;
