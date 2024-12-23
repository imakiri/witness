CREATE SCHEMA witness;

CREATE TABLE witness.events
(
    event_id     uuid         NOT NULL PRIMARY KEY,
    event_date   timestamp    NOT NULL DEFAULT NOW(),
    event_type   int8         NOT NULL,
    event_name   varchar(127) NOT NULL,
    event_caller varchar(127) NOT NULL
);

CREATE UNIQUE INDEX events_event_id ON witness.events (event_id DESC);

CREATE TABLE witness.spans
(
    event_id       uuid      NOT NULL REFERENCES witness.events (event_id),
--     event_date     timestamp NOT NULL DEFAULT NOW(),
    parent_span_id uuid      NOT NULL,
    child_span_id  uuid
);

CREATE INDEX spans_event_id ON witness.spans (event_id DESC);
CREATE INDEX spans_span_id ON witness.spans (child_span_id DESC NULLS LAST) INCLUDE (parent_span_id);

CREATE TABLE witness.records
(
    event_id     uuid      NOT NULL REFERENCES witness.events (event_id),
--     event_date   timestamp NOT NULL DEFAULT NOW(),
    record_name  varchar(127),
    record_value varchar(1022)
);

CREATE INDEX records_event_id ON witness.records (event_id DESC);

-- -- list all span names for last 2 days
-- SELECT e.event_name
-- FROM witness.events e
-- WHERE e.event_type IN (11, 21, 23)
--   AND e.event_date > NOW() - '2 days'::interval
-- GROUP BY e.event_name;
--
-- -- list all spans for given name and time interval
-- SELECT DISTINCT ON (s.parent_span_id, s.child_span_id) s.parent_span_id, s.child_span_id, e.event_date
-- FROM witness.events e
--          INNER JOIN witness.spans s ON e.event_id = s.event_id
-- WHERE e.event_type IN (11, 21, 23)
--   AND e.event_name = 'foo'
--   AND e.event_date > NOW() - '2 days'::interval
-- ORDER BY s.parent_span_id DESC, e.event_id DESC;
--
-- -- list all events for given span
-- SELECT e.event_id, e.event_date, e.event_type, e.event_name, e.event_caller
-- FROM witness.events e
--          INNER JOIN witness.spans s ON e.event_id = s.event_id
-- WHERE s.parent_span_id = '00000000-0000-0000-0000-000000000000';
--
-- -- list all
-- WITH data AS (SELECT e.event_id, s.parent_span_id, COUNT(s.parent_span_id) OVER (PARTITION BY e.event_id) AS total
--               FROM witness.events e
--                        INNER JOIN witness.spans s ON e.event_id = s.event_id)
-- SELECT data.parent_span_id
-- FROM witness.events e
--          INNER JOIN data ON e.event_id = data.event_id AND data.total > 1;