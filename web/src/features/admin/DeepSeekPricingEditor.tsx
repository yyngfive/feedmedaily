import {Button} from "@heroui/react";
import React from "react";

import {TextInputField} from "../../shared/components/FormFields";
import type {SettingsConfigField, SettingsConfigUpdate} from "../../shared/types";

const pricingRows = [
  {model: "V4 Flash", tier: "Off-peak", prefix: "SCIRSS_DEEPSEEK_FLASH_OFF_PEAK"},
  {model: "V4 Flash", tier: "Peak", prefix: "SCIRSS_DEEPSEEK_FLASH_PEAK"},
  {model: "V4 Pro", tier: "Off-peak", prefix: "SCIRSS_DEEPSEEK_PRO_OFF_PEAK"},
  {model: "V4 Pro", tier: "Peak", prefix: "SCIRSS_DEEPSEEK_PRO_PEAK"},
  {model: "GLM-5.3-Flash", tier: "Current promotion", prefix: "SCIRSS_GLM_53_FLASH"},
] as const;

const priceColumns = [
  {label: "Cache hit", suffix: "CACHE_HIT_CNY_PER_MILLION"},
  {label: "Cache miss", suffix: "CACHE_MISS_CNY_PER_MILLION"},
  {label: "Output", suffix: "OUTPUT_CNY_PER_MILLION"},
] as const;

// DeepSeek 定价用紧凑表格编辑，保存后的费率只用于后续新任务。
export function DeepSeekPricingEditor({
  fields,
  saving,
  onSave,
}: {
  fields: SettingsConfigField[];
  saving: boolean;
  onSave: (fields: Record<string, SettingsConfigUpdate>) => Promise<void>;
}) {
  const [values, setValues] = React.useState<Record<string, string>>({});
  const fieldsByKey = React.useMemo(
    () => new Map(fields.map((field) => [field.key, field])),
    [fields],
  );

  React.useEffect(() => {
    setValues(Object.fromEntries(fields.map((field) => [field.key, field.value ?? field.default_value ?? ""])));
  }, [fields]);

  const submit = async () => {
    await onSave(Object.fromEntries(
      fields
        .filter((field) => field.source !== "environment")
        .map((field) => [field.key, {value: values[field.key] ?? ""}]),
    ));
  };
  const hasEnvironmentOverride = fields.some((field) => field.source === "environment");

  return (
    <section className="border-t border-(--line) pt-5">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="max-w-3xl space-y-1">
          <h3 className="text-sm font-semibold text-(--ink)">Token pricing</h3>
          <p className="text-sm leading-6 text-muted">
            CNY per 1M tokens. DeepSeek uses Beijing-time peak/off-peak rates. GLM defaults to
            the current 50% promotional rates (cache hit 0.115, input 0.4, output 1.4); update them here when the promotion ends.
          </p>
          <p className="text-xs leading-5 text-muted">
            Changes apply only to jobs started after saving. Existing usage records keep their saved price snapshots.
          </p>
          {hasEnvironmentOverride ? (
            <p className="text-xs leading-5 text-amber-700">
              One or more values are controlled by system environment variables and cannot be overridden here.
            </p>
          ) : null}
        </div>
        <Button isDisabled={saving || fields.length === 0} size="sm" onPress={() => void submit()}>
          {saving ? "Saving..." : "Save pricing"}
        </Button>
      </div>

      <div className="mt-4 overflow-x-auto">
        <table className="w-full min-w-[720px] border-collapse text-left text-sm">
          <thead className="border-b border-(--line) text-muted">
            <tr>
              <th className="py-2 pr-3 font-medium">Model</th>
              <th className="py-2 pr-3 font-medium">Tier</th>
              {priceColumns.map((column) => <th key={column.suffix} className="px-2 py-2 font-medium">{column.label}</th>)}
            </tr>
          </thead>
          <tbody className="divide-y divide-(--line)">
            {pricingRows.map((row) => (
              <tr key={row.prefix}>
                <td className="py-3 pr-3 font-medium text-(--ink)">{row.model}</td>
                <td className="py-3 pr-3 text-muted">{row.tier}</td>
                {priceColumns.map((column) => {
                  const key = `${row.prefix}_${column.suffix}`;
                  const field = fieldsByKey.get(key);
                  return (
                    <td key={key} className="px-2 py-3">
                      {field ? (
                        <TextInputField
                          hideLabel
                          disabled={field.source === "environment"}
                          inputMode="decimal"
                          label={`${row.model} ${row.tier} ${column.label}`}
                          placeholder={field.default_value ?? ""}
                          value={values[key] ?? ""}
                          onChange={(value) => setValues((current) => ({...current, [key]: value}))}
                        />
                      ) : null}
                    </td>
                  );
                })}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
