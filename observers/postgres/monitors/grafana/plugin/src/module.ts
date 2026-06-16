import { DataSourcePlugin } from '@grafana/data';
import { WitnessDataSource } from './datasource';
import { ConfigEditor } from './components/ConfigEditor';
import { QueryEditor } from './components/QueryEditor';
import {
  WitnessDataSourceOptions,
  WitnessQuery,
} from './types';

export const plugin = new DataSourcePlugin<
  WitnessDataSource,
  WitnessQuery,
  WitnessDataSourceOptions
>(WitnessDataSource)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor);
