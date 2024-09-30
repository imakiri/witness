# Witness

#### _Better than OTEL_

---

## Data model

![Witness model](docs/witness.drawio.svg)

```json
{
  "spans": {
    "trace_id": "uuid",
    "span_id": "uuid",
    "span_name": "string",
    "span_event_type": "span_event_types",
    "span_event_date": "timestamp"
  },
  "logs": {
    "trace_id": "uuid",
    "span_id": "uuid",
    "log_id": "uuid",
    "log_event_date": "timestamp",
    "log_event_type": "log_event_types"
  },
  "records": {
    "trace_id": "uuid",
    "span_id": "uuid",
    "log_id": "uuid",
    "record_name": "string",
    "record_value": "string"
  }
}
```

