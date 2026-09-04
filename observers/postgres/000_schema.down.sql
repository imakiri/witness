-- Reverse of 000_schema.up.sql. Drops the witness schema with all of its
-- tables, indexes and views. The pg_trgm extension is left in place: it is a
-- database-global resource other schemas may rely on.

DROP SCHEMA witness CASCADE;
