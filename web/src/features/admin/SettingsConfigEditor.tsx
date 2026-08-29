import {Button} from "@heroui/react";
import React from "react";

import {TextInputField} from "../../shared/components/FormFields";
import {SelectField} from "../../shared/components/SelectField";
import type {SettingsConfigField, SettingsConfigUpdate} from "../../shared/types";

function fieldSourceSummary(field: SettingsConfigField): string {
  const storageLabel = field.storage_label ?? "local configuration";
  if (field.secret) {
    if (!field.configured) {
      return "Not configured yet.";
    }
    if (field.source === "environment") {
      return field.stored_in_dotenv
        ? `Configured in the system environment. A local value in ${storageLabel} also exists but is currently overridden.`
        : "Configured in the system environment. Local changes will not take effect until the override is removed.";
    }
    if (field.source === "dotenv") {
      return "Stored in the local .env file.";
    }
    if (field.source === "secret_store") {
      return `Stored in ${storageLabel}.`;
    }
    if (field.source === "settings") {
      return `Stored in ${storageLabel}.`;
    }
    return "Configured from a default or inherited source.";
  }
  if (field.source === "environment") {
    return field.stored_in_dotenv
      ? `Current value comes from the system environment. A local entry in ${storageLabel} is present but overridden.`
      : "Current value comes from the system environment.";
  }
  if (field.source === "dotenv") {
    return "Current value comes from the local .env file.";
  }
  if (field.source === "settings" || field.source === "secret_store") {
    return `Current value comes from ${storageLabel}.`;
  }
  if (field.source === "default") {
    return `Using the built-in default${field.default_value ? `: ${field.default_value}` : "."}`;
  }
  return "Unset.";
}

function toFieldGroups(fields: SettingsConfigField[]) {
  const order: string[] = [];
  const groups = new Map<string, SettingsConfigField[]>();
  fields.forEach((field) => {
    if (!groups.has(field.section)) {
      groups.set(field.section, []);
      order.push(field.section);
    }
    groups.get(field.section)?.push(field);
  });
  return order.map((section) => ({section, fields: groups.get(section) ?? []}));
}

export type SettingsConfigEditorHandle = {
  getPayload: () => Record<string, SettingsConfigUpdate>;
};

export const SettingsConfigEditor = React.forwardRef<SettingsConfigEditorHandle, {
  fields: SettingsConfigField[];
  hideGroupTitles?: boolean;
  intro?: React.ReactNode;
  saving: boolean;
  saveLabel?: string;
  showHeader?: boolean;
  showSaveAction?: boolean;
  title?: string;
  onSave: (fields: Record<string, SettingsConfigUpdate>) => Promise<void>;
}>(function SettingsConfigEditor({
  fields,
  hideGroupTitles = false,
  intro,
  saving,
  saveLabel = "Save local settings",
  showHeader = true,
  showSaveAction = true,
  title = "Local configuration",
  onSave,
}, ref) {
  const [values, setValues] = React.useState<Record<string, string>>({});
  const [secretValues, setSecretValues] = React.useState<Record<string, string>>({});
  const [secretClears, setSecretClears] = React.useState<Record<string, boolean>>({});

  React.useEffect(() => {
    setValues(
      Object.fromEntries(
        fields
          .filter((field) => !field.secret)
          .map((field) => [field.key, field.value ?? ""]),
      ),
    );
    setSecretValues(
      Object.fromEntries(fields.filter((field) => field.secret).map((field) => [field.key, ""])),
    );
    setSecretClears(
      Object.fromEntries(fields.filter((field) => field.secret).map((field) => [field.key, false])),
    );
  }, [fields]);

  const groups = React.useMemo(() => toFieldGroups(fields), [fields]);

  const buildPayload = React.useCallback(() => {
    const payload: Record<string, SettingsConfigUpdate> = {};
    fields.forEach((field) => {
      if (field.source === "environment") return;
      if (field.secret) {
        const nextSecret = secretValues[field.key]?.trim() ?? "";
        const clear = Boolean(secretClears[field.key]);
        if (nextSecret || clear) {
          payload[field.key] = {value: nextSecret || undefined, clear};
        }
        return;
      }
      payload[field.key] = {value: values[field.key] ?? ""};
    });
    return payload;
  }, [fields, secretClears, secretValues, values]);

  React.useImperativeHandle(ref, () => ({getPayload: buildPayload}), [buildPayload]);

  const submit = async () => onSave(buildPayload());

  return (
    <div className="space-y-4">
      {showHeader ? <div className="border-b border-(--line) pb-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="max-w-3xl space-y-2">
            <h3 className="text-sm font-semibold text-(--ink)">{title}</h3>
            <p className="text-sm leading-6 text-muted">
              Secret fields are never sent back in plain text. In source mode this writes to the
              local <code>.env</code>; in release mode it writes to the per-user FeedMeDaily
              settings store.
            </p>
            {intro}
          </div>
          {showSaveAction ? (
            <Button isDisabled={saving} size="sm" onPress={() => void submit()}>
              {saving ? "Saving..." : saveLabel}
            </Button>
          ) : null}
        </div>
      </div> : null}

      {groups.map((group) => (
        <section key={group.section} className="border-b border-(--line) pb-5 last:border-b-0">
          {hideGroupTitles ? null : <h3 className="text-sm font-semibold text-(--ink)">{group.section}</h3>}
          <div className={`${hideGroupTitles ? "" : "mt-3 "}divide-y divide-(--line)`}>
            {group.fields.map((field) => {
              const value = values[field.key] ?? "";
              const secretValue = secretValues[field.key] ?? "";
              const secretClear = Boolean(secretClears[field.key]);
              return (
                <div
                  key={field.key}
                  className="grid gap-3 py-4 md:grid-cols-[minmax(0,0.75fr)_minmax(260px,1fr)] md:items-start"
                >
                  <div className="space-y-1">
                    <span className="block text-sm font-medium text-(--ink)">{field.label}</span>
                    <span className="block text-sm leading-6 text-muted">{field.description}</span>
                  </div>
                  <div className="min-w-0">
                    {field.input_type === "select" ? (
                      <SelectField
                        disabled={field.source === "environment"}
                        hideLabel
                        id={`settings-${field.key}`}
                        label={field.label}
                        options={field.options}
                        value={value}
                        onChange={(nextValue) =>
                          setValues((current) => ({...current, [field.key]: nextValue}))
                        }
                      />
                    ) : field.secret ? (
                      <>
                        <TextInputField
                          disabled={field.source === "environment"}
                          hideLabel
                          label={field.label}
                          placeholder={
                            field.configured ? "Leave blank to keep the current secret" : "Paste a new secret"
                          }
                          type="password"
                          value={secretValue}
                          onChange={(nextValue) => {
                            setSecretValues((current) => ({
                              ...current,
                              [field.key]: nextValue,
                            }));
                            if (nextValue.trim()) {
                              setSecretClears((current) => ({...current, [field.key]: false}));
                            }
                          }}
                        />
                        <div className="mt-2 flex flex-wrap items-center gap-3 text-xs leading-5 text-muted">
                          <span>{fieldSourceSummary(field)}</span>
                          {field.configured || field.stored_in_dotenv ? (
                            <Button
                              size="sm"
                              variant={secretClear ? "secondary" : "outline"}
                              onPress={() =>
                                setSecretClears((current) => ({
                                  ...current,
                                  [field.key]: !current[field.key],
                                }))
                              }
                            >
                              {secretClear ? "Will clear on save" : "Clear stored value"}
                            </Button>
                          ) : null}
                        </div>
                      </>
                    ) : (
                      <>
                        <TextInputField
                          disabled={field.source === "environment"}
                          hideLabel
                          inputMode={field.input_type === "number" ? "numeric" : undefined}
                          label={field.label}
                          placeholder={field.default_value ?? ""}
                          type={field.input_type === "number" ? "number" : field.input_type === "url" ? "url" : "text"}
                          value={value}
                          onChange={(nextValue) =>
                            setValues((current) => ({...current, [field.key]: nextValue}))
                          }
                        />
                        <span className="mt-2 block text-xs leading-5 text-muted">
                          {fieldSourceSummary(field)}
                        </span>
                      </>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
        </section>
      ))}
    </div>
  );
});
