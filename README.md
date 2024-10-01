# Witness

#### _Better than OTEL_

---

## Data model

![Witness model](docs/witness.drawio.svg)

```json
{
  "ctx": {
    "trace_id": "uuid",
    "instance_id": "uuid",
    "span_id": "uuid"
  },
  "database": {
    "events": {
      "trace_id": "uuid",
      "instance_id": "uuid",
      "span_id": "uuid",
      "event_id": "uuid",
      "event_date": "timestamp",
      "event_type": "event_types",
      "event_name": "event_name"
    },
    "records": {
      "event_id": "uuid",
      "record_name": "string",
      "record_value": "string"
    }
  }
}

```

