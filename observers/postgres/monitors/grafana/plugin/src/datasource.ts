import { DataSourceInstanceSettings, ScopedVars } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';
import {
  EventTypeOption,
  NameOption,
  WitnessDataSourceOptions,
  WitnessQuery,
} from './types';

export class WitnessDataSource extends DataSourceWithBackend<
  WitnessQuery,
  WitnessDataSourceOptions
> {
  constructor(instanceSettings: DataSourceInstanceSettings<WitnessDataSourceOptions>) {
    super(instanceSettings);
  }

  applyTemplateVariables(query: WitnessQuery, scopedVars: ScopedVars): WitnessQuery {
    const srv = getTemplateSrv();
    const interp = (v?: string) => (v ? srv.replace(v, scopedVars) : v);

    return {
      ...query,
      search: query.search && {
        ...query.search,
        spanID: interp(query.search.spanID),
        eventID: interp(query.search.eventID),
        traceID: interp(query.search.traceID),
        service: interp(query.search.service),
        message: interp(query.search.message),
        caller: interp(query.search.caller),
      },
      trace: query.trace && { traceID: interp(query.trace.traceID) ?? '' },
      logs: query.logs && {
        ...query.logs,
        service: interp(query.logs.service),
        traceID: interp(query.logs.traceID),
        caller: interp(query.logs.caller),
        message: interp(query.logs.message),
      },
      table: query.table && { ...query.table, nameLike: interp(query.table.nameLike) },
      traces: query.traces && {
        service: interp(query.traces.service),
        search: interp(query.traces.search),
      },
      serviceMap: query.serviceMap && { traceID: interp(query.serviceMap.traceID) ?? '' },
    };
  }

  async getEventTypes(): Promise<EventTypeOption[]> {
    return this.getResource('event-types');
  }

  async getServices(): Promise<NameOption[]> {
    return this.getResource('services');
  }

  async getOperations(service?: string): Promise<NameOption[]> {
    const path = service ? `operations?service=${encodeURIComponent(service)}` : 'operations';
    return this.getResource(path);
  }
}
