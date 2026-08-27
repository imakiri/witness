import React, { ChangeEvent, useEffect, useState } from 'react';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import {
  InlineField,
  InlineFieldRow,
  Input,
  Select,
  MultiSelect,
  Switch,
  Button,
  IconButton,
} from '@grafana/ui';
import { WitnessDataSource } from '../datasource';
import {
  EventTypeOption,
  LogsParams,
  QueryType,
  RecordFilter,
  SearchParams,
  ServiceMapParams,
  TableParams,
  TraceParams,
  TracesParams,
  WitnessDataSourceOptions,
  WitnessQuery,
  defaultQuery,
} from '../types';

type Props = QueryEditorProps<WitnessDataSource, WitnessQuery, WitnessDataSourceOptions>;

const QUERY_TYPES: Array<SelectableValue<QueryType>> = [
  { label: 'Search (all events)', value: 'search', description: 'Universal filter by id/text/event-type/records' },
  { label: 'Trace (distributed)', value: 'trace', description: 'Reconstruct a single trace by root span_id' },
  { label: 'Logs', value: 'logs', description: 'Logs panel: log:* + error:* events' },
  { label: 'Table (spans)', value: 'table', description: 'Recent spans with duration' },
  { label: 'Traces (recent)', value: 'traces', description: 'List recent traces with services involved + event/error counts' },
  { label: 'Service map', value: 'service-map', description: 'Inter-service call graph for one trace (NodeGraph)' },
];

const LBL = 14;

export function QueryEditor(props: Props) {
  const { query, onChange, onRunQuery, datasource } = props;
  const q: WitnessQuery = { ...defaultQuery, ...query };

  const [eventTypes, setEventTypes] = useState<EventTypeOption[]>([]);
  useEffect(() => {
    datasource.getEventTypes().then(setEventTypes).catch(() => setEventTypes([]));
  }, [datasource]);

  const eventTypeOptions: Array<SelectableValue<number>> = eventTypes.map((e) => ({
    label: `${e.name} (${e.event_type})`,
    value: e.event_type,
  }));

  const setQ = (patch: Partial<WitnessQuery>) => onChange({ ...q, ...patch });

  return (
    <div>
      <InlineFieldRow>
        <InlineField label="Query type" labelWidth={LBL}>
          <Select
            width={32}
            value={QUERY_TYPES.find((t) => t.value === q.queryType)}
            options={QUERY_TYPES}
            onChange={(v) => setQ({ queryType: v.value ?? 'search' })}
          />
        </InlineField>
        <InlineField label="Limit" labelWidth={10}>
          <Input
            type="number"
            width={16}
            value={q.limit ?? 500}
            onChange={(e: ChangeEvent<HTMLInputElement>) =>
              setQ({ limit: parseInt(e.target.value, 10) || 500 })
            }
          />
        </InlineField>
        <Button variant="secondary" size="md" onClick={() => onRunQuery()}>
          Run
        </Button>
      </InlineFieldRow>

      {q.queryType === 'search' && (
        <SearchEditor
          value={q.search ?? {}}
          eventTypeOptions={eventTypeOptions}
          onChange={(s) => setQ({ search: s })}
        />
      )}
      {q.queryType === 'trace' && (
        <TraceEditor value={q.trace ?? { traceID: '' }} onChange={(t) => setQ({ trace: t })} />
      )}
      {q.queryType === 'logs' && (
        <LogsEditor
          value={q.logs ?? {}}
          eventTypeOptions={eventTypeOptions}
          onChange={(l) => setQ({ logs: l })}
        />
      )}
      {q.queryType === 'table' && (
        <TableEditor value={q.table ?? {}} onChange={(t) => setQ({ table: t })} />
      )}
      {q.queryType === 'traces' && (
        <TracesEditor value={q.traces ?? {}} onChange={(t) => setQ({ traces: t })} />
      )}
      {q.queryType === 'service-map' && (
        <ServiceMapEditor
          value={q.serviceMap ?? { traceID: '' }}
          onChange={(sm) => setQ({ serviceMap: sm })}
        />
      )}
    </div>
  );
}

function TracesEditor(props: { value: TracesParams; onChange: (t: TracesParams) => void }) {
  const { value, onChange } = props;
  const set = (patch: Partial<TracesParams>) => onChange({ ...value, ...patch });
  return (
    <InlineFieldRow>
      <InlineField label="Service" labelWidth={LBL} grow>
        <Input
          value={value.service ?? ''}
          placeholder="exact service name — only traces that touched it"
          onChange={(e: ChangeEvent<HTMLInputElement>) => set({ service: e.target.value })}
        />
      </InlineField>
      <InlineField label="Search" labelWidth={LBL} grow>
        <Input
          value={value.search ?? ''}
          placeholder="ILIKE on instance:online event_message"
          onChange={(e: ChangeEvent<HTMLInputElement>) => set({ search: e.target.value })}
        />
      </InlineField>
    </InlineFieldRow>
  );
}

function SearchEditor(props: {
  value: SearchParams;
  eventTypeOptions: Array<SelectableValue<number>>;
  onChange: (s: SearchParams) => void;
}) {
  const { value, eventTypeOptions, onChange } = props;
  const set = (patch: Partial<SearchParams>) => onChange({ ...value, ...patch });

  const records: RecordFilter[] = value.records ?? [];
  const setRecord = (i: number, patch: Partial<RecordFilter>) => {
    const next = records.slice();
    next[i] = { ...next[i], ...patch };
    set({ records: next });
  };
  const addRecord = () => set({ records: [...records, { key: '', value: '', op: 'eq' }] });
  const removeRecord = (i: number) => {
    const next = records.slice();
    next.splice(i, 1);
    set({ records: next });
  };

  return (
    <>
      <InlineFieldRow>
        <InlineField label="Event ID" labelWidth={LBL} grow>
          <Input
            value={value.eventID ?? ''}
            placeholder="uuid"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ eventID: e.target.value })}
          />
        </InlineField>
        <InlineField label="Span ID" labelWidth={LBL} grow>
          <Input
            value={value.spanID ?? ''}
            placeholder="uuid"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ spanID: e.target.value })}
          />
        </InlineField>
      </InlineFieldRow>
      <InlineFieldRow>
        <InlineField label="Trace ID" labelWidth={LBL} grow>
          <Input
            value={value.traceID ?? ''}
            placeholder="uuid (root span_id of originating instance)"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ traceID: e.target.value })}
          />
        </InlineField>
        <InlineField label="Service" labelWidth={LBL} grow>
          <Input
            value={value.service ?? ''}
            placeholder="exact service name (e.g. service-a)"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ service: e.target.value })}
          />
        </InlineField>
      </InlineFieldRow>
      <InlineFieldRow>
        <InlineField label="Message" labelWidth={LBL} grow>
          <Input
            value={value.message ?? ''}
            placeholder="full-text / substring"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ message: e.target.value })}
          />
        </InlineField>
        <InlineField label="Caller" labelWidth={LBL} grow>
          <Input
            value={value.caller ?? ''}
            placeholder="package.Func or file:line"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ caller: e.target.value })}
          />
        </InlineField>
      </InlineFieldRow>
      <InlineFieldRow>
        <InlineField label="Event types" labelWidth={LBL} grow>
          <MultiSelect
            options={eventTypeOptions}
            value={(value.eventTypes ?? []).map((v) => ({ value: v, label: String(v) }))}
            onChange={(opts) => set({ eventTypes: opts.map((o) => o.value!).filter((v) => v != null) })}
            placeholder="any"
          />
        </InlineField>
      </InlineFieldRow>
      <div style={{ marginTop: 8 }}>
        <div style={{ marginBottom: 4, fontWeight: 500 }}>Record filters</div>
        {records.map((r, i) => (
          <InlineFieldRow key={i}>
            <InlineField label="Key" labelWidth={6}>
              <Input
                value={r.key}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setRecord(i, { key: e.target.value })}
              />
            </InlineField>
            <InlineField label="Op" labelWidth={4}>
              <Select
                width={14}
                options={[
                  { label: '=', value: 'eq' },
                  { label: 'ILIKE', value: 'ilike' },
                ]}
                value={{ value: r.op, label: r.op === 'eq' ? '=' : 'ILIKE' }}
                onChange={(v) => setRecord(i, { op: (v.value as 'eq' | 'ilike') ?? 'eq' })}
              />
            </InlineField>
            <InlineField label="Value" labelWidth={6} grow>
              <Input
                value={r.value}
                onChange={(e: ChangeEvent<HTMLInputElement>) => setRecord(i, { value: e.target.value })}
              />
            </InlineField>
            <IconButton name="trash-alt" tooltip="Remove" onClick={() => removeRecord(i)} />
          </InlineFieldRow>
        ))}
        <Button variant="secondary" size="sm" onClick={addRecord} icon="plus">
          Add record filter
        </Button>
      </div>
    </>
  );
}

function TraceEditor(props: { value: TraceParams; onChange: (t: TraceParams) => void }) {
  return (
    <InlineFieldRow>
      <InlineField label="Trace ID" labelWidth={LBL} grow>
        <Input
          value={props.value.traceID ?? ''}
          placeholder="uuid of the originating instance's root span"
          onChange={(e: ChangeEvent<HTMLInputElement>) =>
            props.onChange({ traceID: e.target.value })
          }
        />
      </InlineField>
    </InlineFieldRow>
  );
}

function LogsEditor(props: {
  value: LogsParams;
  eventTypeOptions: Array<SelectableValue<number>>;
  onChange: (l: LogsParams) => void;
}) {
  const { value, eventTypeOptions, onChange } = props;
  const set = (patch: Partial<LogsParams>) => onChange({ ...value, ...patch });

  return (
    <>
      <InlineFieldRow>
        <InlineField label="Trace ID" labelWidth={LBL} grow>
          <Input
            value={value.traceID ?? ''}
            placeholder="uuid — only events of this trace"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ traceID: e.target.value })}
          />
        </InlineField>
        <InlineField label="Service" labelWidth={LBL} grow>
          <Input
            value={value.service ?? ''}
            placeholder="exact service name"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ service: e.target.value })}
          />
        </InlineField>
      </InlineFieldRow>
      <InlineFieldRow>
        <InlineField label="Message" labelWidth={LBL} grow>
          <Input
            value={value.message ?? ''}
            placeholder="substring"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ message: e.target.value })}
          />
        </InlineField>
        <InlineField label="Caller" labelWidth={LBL} grow>
          <Input
            value={value.caller ?? ''}
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ caller: e.target.value })}
          />
        </InlineField>
      </InlineFieldRow>
      <InlineFieldRow>
        <InlineField label="Event types" labelWidth={LBL} grow>
          <MultiSelect
            options={eventTypeOptions}
            value={(value.eventTypes ?? []).map((v) => ({ value: v, label: String(v) }))}
            onChange={(opts) => set({ eventTypes: opts.map((o) => o.value!).filter((v) => v != null) })}
            placeholder="default: log:* + error:*"
          />
        </InlineField>
      </InlineFieldRow>
    </>
  );
}

function ServiceMapEditor(props: { value: ServiceMapParams; onChange: (sm: ServiceMapParams) => void }) {
  return (
    <InlineFieldRow>
      <InlineField label="Trace ID" labelWidth={LBL} grow>
        <Input
          value={props.value.traceID ?? ''}
          placeholder="uuid of the trace to render as service-graph"
          onChange={(e: ChangeEvent<HTMLInputElement>) =>
            props.onChange({ traceID: e.target.value })
          }
        />
      </InlineField>
    </InlineFieldRow>
  );
}

function TableEditor(props: { value: TableParams; onChange: (t: TableParams) => void }) {
  const { value, onChange } = props;
  const set = (patch: Partial<TableParams>) => onChange({ ...value, ...patch });
  return (
    <>
      <InlineFieldRow>
        <InlineField label="Only roots" labelWidth={LBL} tooltip="Show only top-level spans with no parent">
          <Switch value={!!value.onlyRoots} onChange={(e) => set({ onlyRoots: e.currentTarget.checked })} />
        </InlineField>
        <InlineField label="Name like" labelWidth={LBL} grow>
          <Input
            value={value.nameLike ?? ''}
            placeholder="ILIKE pattern (e.g. handle%)"
            onChange={(e: ChangeEvent<HTMLInputElement>) => set({ nameLike: e.target.value })}
          />
        </InlineField>
      </InlineFieldRow>
    </>
  );
}
