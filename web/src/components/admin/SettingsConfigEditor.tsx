import {Button} from "@heroui/react";
import React from "react";

import type {SettingsConfigField, SettingsConfigUpdate} from "../../types";

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

export function SettingsConfigEditor({
  fields,
  intro,
  saving,
  saveLabel = "Save local settings",
  onSave,
}: {
  fields: SettingsConfigField[];
  intro?: React.ReactNode;
  saving: boolean;
  saveLabel?: string;
  onSave: (fields: Record<string, SettingsConfigUpdate>) => Promise<void>;
}) {
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

  const submit = async () => {
    const payload: Record<string, SettingsConfigUpdate> = {};
    fields.forEach((field) => {
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
    await onSave(payload);
  };

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-(--line) bg-(--paper-accent) p-4">
        <div className="flex flex-wrap items-start justify-between gap-3">
          <div className="max-w-3xl space-y-2">
            <h3 className="text-sm font-semibold text-(--ink)">Local configuration</h3>
            <p className="text-sm leading-6 text-muted">
              Secret fields are never sent back in plain text. In source mode this writes to the
              local <code>.env</code>; in release mode it writes to the per-user FeedMeDaily
              settings store.
            </p>
            {intro}
          </div>
          <Button isDisabled={saving} size="sm" onPress={() => void submit()}>
            {saving ? "Saving..." : saveLabel}
          </Button>
        </div>
      </div>

      {groups.map((group) => (
        <section
          key={group.section}
          className="rounded-lg border border-(--line) bg-(--paper-accent) p-4"
        >
          <h3 className="text-sm font-semibold text-(--ink)">{group.section}</h3>
          <div className="mt-3 grid gap-4 md:grid-cols-2">
            {group.fields.map((field) => {
              const value = values[field.key] ?? "";
              const secretValue = secretValues[field.key] ?? "";
              const secretClear = Boolean(secretClears[field.key]);
              return (
                <label key={field.key} className="block rounded-lg border border-(--line) p-3">
                  <span className="text-sm font-medium text-(--ink)">{field.label}</span>
                  <span className="mt-1 block text-sm leading-6 text-muted">
                    {field.description}
                  </span>
                  {field.input_type === "select" ? (
                    <select
                      aria-label={field.label}
                      className="mt-3 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                      value={value}
                      onChange={(event) =>
                        setValues((current) => ({...current, [field.key]: event.target.value}))
                      }
                    >
                      {field.options.map((option) => (
                        <option key={option.value} value={option.value}>
                          {option.label}
                        </option>
                      ))}
                    </select>
                  ) : field.secret ? (
                    <>
                      <input
                        aria-label={field.label}
                        className="mt-3 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink) placeholder:text-muted"
                        placeholder={
                          field.configured ? "Leave blank to keep the current secret" : "Paste a new secret"
                        }
                        type="password"
                        value={secretValue}
                        onChange={(event) => {
                          setSecretValues((current) => ({
                            ...current,
                            [field.key]: event.target.value,
                          }));
                          if (event.target.value.trim()) {
                            setSecretClears((current) => ({...current, [field.key]: false}));
                          }
                        }}
                      />
                      <div className="mt-3 flex flex-wrap items-center gap-3 text-sm text-muted">
                        <span>{fieldSourceSummary(field)}</span>
                        {field.configured || field.stored_in_dotenv ? (
                          <button
                            className={`rounded-full border px-2 py-1 ${
                              secretClear
                                ? "border-[--danger-line] bg-[--danger-bg] text-[--danger-ink]"
                                : "border-(--line) text-(--subtle-ink)"
                            }`}
                            type="button"
                            onClick={() =>
                              setSecretClears((current) => ({
                                ...current,
                                [field.key]: !current[field.key],
                              }))
                            }
                          >
                            {secretClear ? "Will clear on save" : "Clear stored value"}
                          </button>
                        ) : null}
                      </div>
                    </>
                  ) : (
                    <input
                      aria-label={field.label}
                      className="mt-3 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink) placeholder:text-muted"
                      inputMode={field.input_type === "number" ? "numeric" : undefined}
                      placeholder={field.default_value ?? ""}
                      type={field.input_type === "number" ? "number" : "text"}
                      value={value}
                      onChange={(event) =>
                        setValues((current) => ({...current, [field.key]: event.target.value}))
                      }
                    />
                  )}
                  {!field.secret ? (
                    <span className="mt-3 block text-sm leading-6 text-muted">
                      {fieldSourceSummary(field)}
                    </span>
                  ) : null}
                </label>
              );
            })}
          </div>
        </section>
      ))}
    </div>
  );
}
