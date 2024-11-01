CREATE SCHEMA witness;

CREATE TABLE witness.events
(
    span_id       uuid         NOT NULL,
    span_type     int8         NOT NULL,
    event_id      uuid         NOT NULL PRIMARY KEY,
    event_date    timestamp    NOT NULL DEFAULT NOW(),
    event_type    int8         NOT NULL,
    event_name    varchar(127) NOT NULL,
    event_caller  varchar(127) NOT NULL,
    event_version varchar(31)  NOT NULL
);

CREATE UNIQUE INDEX witness.events_event_id ON witness.events (event_id DESC);

CREATE INDEX witness.events_span_id ON witness.events (span_id DESC) INCLUDE (event_type);

CREATE TABLE witness.records
(
    event_id     uuid NOT NULL REFERENCES witness.events (event_id),
    record_id    uuid PRIMARY KEY,
    record_name  varchar(127),
    record_value varchar
);

CREATE INDEX witness.records_event_id ON witness.records (event_id DESC);