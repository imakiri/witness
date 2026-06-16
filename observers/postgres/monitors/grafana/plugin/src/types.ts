import { DataQuery, DataSourceJsonData } from '@grafana/data';

export type QueryType = 'search' | 'trace' | 'logs' | 'table' | 'traces' | 'service-map';

export interface RecordFilter {
  key: string;
  value: string;
  op: 'eq' | 'ilike';
}

export interface SearchParams {
  spanID?: string;
  eventID?: string;
  traceID?: string;
  service?: string;
  message?: string;
  caller?: string;
  eventTypes?: number[];
  records?: RecordFilter[];
}

export interface TraceParams {
  traceID: string;
}

export interface LogsParams {
  eventTypes?: number[];
  service?: string;
  traceID?: string;
  caller?: string;
  message?: string;
}

export interface TableParams {
  onlyRoots?: boolean;
  nameLike?: string;
}

export interface TracesParams {
  service?: string;
  search?: string;
}

export interface ServiceMapParams {
  traceID: string;
}

export interface WitnessQuery extends DataQuery {
  queryType: QueryType;
  limit?: number;
  search?: SearchParams;
  trace?: TraceParams;
  logs?: LogsParams;
  table?: TableParams;
  traces?: TracesParams;
  serviceMap?: ServiceMapParams;
}

export const defaultQuery: Partial<WitnessQuery> = {
  queryType: 'search',
  limit: 500,
  search: {},
};

export interface WitnessDataSourceOptions extends DataSourceJsonData {
  url?: string;
  maxConns?: number;
}

export interface WitnessSecureJsonData {
  password?: string;
}

export interface EventTypeOption {
  event_type: number;
  name: string;
}

export interface NameOption {
  name: string;
}
