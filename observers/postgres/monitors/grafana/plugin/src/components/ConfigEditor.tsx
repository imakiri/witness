import React, { ChangeEvent } from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { InlineField, Input, SecretInput, FieldSet } from '@grafana/ui';
import { WitnessDataSourceOptions, WitnessSecureJsonData } from '../types';

type Props = DataSourcePluginOptionsEditorProps<
  WitnessDataSourceOptions,
  WitnessSecureJsonData
>;

const LABEL_WIDTH = 16;
const INPUT_WIDTH = 50;

export function ConfigEditor(props: Props) {
  const { options, onOptionsChange } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const setJSON = <K extends keyof WitnessDataSourceOptions>(key: K, value: WitnessDataSourceOptions[K]) =>
    onOptionsChange({ ...options, jsonData: { ...jsonData, [key]: value } });

  const onUrl = (e: ChangeEvent<HTMLInputElement>) => setJSON('url', e.target.value);
  const onMaxConns = (e: ChangeEvent<HTMLInputElement>) => {
    const n = parseInt(e.target.value, 10);
    setJSON('maxConns', isNaN(n) ? undefined : n);
  };
  const onPassword = (e: ChangeEvent<HTMLInputElement>) =>
    onOptionsChange({
      ...options,
      secureJsonData: { ...secureJsonData, password: e.target.value },
    });
  const onResetPassword = () =>
    onOptionsChange({
      ...options,
      secureJsonFields: { ...secureJsonFields, password: false },
      secureJsonData: { ...secureJsonData, password: '' },
    });

  return (
    <FieldSet label="Postgres connection">
      <InlineField
        label="URL"
        labelWidth={LABEL_WIDTH}
        tooltip="postgres://user:pass@host:5432/db?sslmode=disable"
      >
        <Input
          width={INPUT_WIDTH}
          value={jsonData.url ?? ''}
          placeholder="postgres://witness@localhost:5432/witness?sslmode=disable"
          onChange={onUrl}
        />
      </InlineField>

      <InlineField
        label="Max connections"
        labelWidth={LABEL_WIDTH}
        tooltip="pgxpool MaxConns (default 4)"
      >
        <Input
          type="number"
          width={INPUT_WIDTH}
          value={jsonData.maxConns ?? ''}
          placeholder="4"
          onChange={onMaxConns}
        />
      </InlineField>

      <InlineField
        label="Password"
        labelWidth={LABEL_WIDTH}
        tooltip="If omitted, the password embedded in URL is used."
      >
        <SecretInput
          width={INPUT_WIDTH}
          value={secureJsonData?.password ?? ''}
          isConfigured={Boolean(secureJsonFields?.password)}
          placeholder="secret"
          onChange={onPassword}
          onReset={onResetPassword}
        />
      </InlineField>
    </FieldSet>
  );
}
