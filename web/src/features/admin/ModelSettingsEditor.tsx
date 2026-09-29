import {Button} from "@heroui/react";
import React from "react";

import {TextInputField} from "../../shared/components/FormFields";
import {SelectField} from "../../shared/components/SelectField";
import {StatusBanner} from "../../shared/components/StatusBanner";
import type {ClassifierModelsResponse, JobInfo, ProfileModelsResponse, SettingsConfigUpdate} from "../../shared/types";

export type ClassifierModelsDraft = {
  enabledModelIds: string[];
  defaultModelId: string;
  credentials: Record<string, SettingsConfigUpdate>;
  reuseDeepSeekKeyForProfile: boolean;
};

export type ProfileModelsDraft = {
  defaultModelId: string;
};

export function createClassifierModelsDraft(response: ClassifierModelsResponse): ClassifierModelsDraft {
  const available = new Set(response.models.filter((model) => model.configured || model.key_optional).map((model) => model.id));
  const selected = response.enabled_model_ids.filter((id) => available.has(id));
  return {
    enabledModelIds: selected,
    defaultModelId: selected.includes(response.default_model_id) ? response.default_model_id : selected[0] ?? "",
    credentials: {},
    reuseDeepSeekKeyForProfile: false,
  };
}

export function enabledClassifierModelIdsAfterKeyChange(
  draft: ClassifierModelsDraft,
  models: ClassifierModelsResponse,
  provider: string,
  hasPendingKey: boolean,
): string[] {
  const providerWasConfigured = models.models.some((model) => model.provider === provider && model.configured);
  return models.models
    .filter((model) => model.provider === provider
      ? (!providerWasConfigured && hasPendingKey) || (draft.enabledModelIds.includes(model.id) && (model.configured || hasPendingKey))
      : draft.enabledModelIds.includes(model.id))
    .map((model) => model.id);
}

export function createProfileModelsDraft(response: ProfileModelsResponse): ProfileModelsDraft {
  return {
    defaultModelId: response.models.some((model) => model.id === response.default_model_id)
      ? response.default_model_id
      : "",
  };
}

export function classifierModelsUpdateFromDraft(draft: ClassifierModelsDraft, models: ClassifierModelsResponse) {
  const credentials: Record<string, SettingsConfigUpdate> = {};
  for (const [provider, credential] of Object.entries(draft.credentials)) {
    const model = models.models.find((item) => item.provider === provider && !item.key_optional);
    if (model) credentials[model.id] = credential;
  }
  return {
    enabled_model_ids: draft.enabledModelIds,
    default_model_id: draft.defaultModelId,
    credentials,
    reuse_deepseek_key_for_profile: draft.reuseDeepSeekKeyForProfile,
  };
}

export function classifierModelsDraftHasRequiredKeys(
  draft: ClassifierModelsDraft,
  models: ClassifierModelsResponse,
): boolean {
  return draft.enabledModelIds.length > 0 && draft.enabledModelIds.every((modelID) => {
    const model = models.models.find((item) => item.id === modelID);
    if (model?.key_optional) return true;
    const credential = model ? draft.credentials[model.provider] : undefined;
    if (credential?.value?.trim()) return true;
    if (credential?.clear) return Boolean(model?.environment_override);
    return Boolean(model?.configured);
  });
}

function testStateForModel(jobs: JobInfo[], modelID: string): JobInfo | null {
  return jobs.find((job) => {
    if (job.job_type !== "model-test" && job.job_type !== "profile-model-test") return false;
    const resultModel = typeof job.result?.model_id === "string" ? job.result.model_id : "";
    return resultModel === modelID || job.message?.includes(modelID) === true;
  }) ?? null;
}

const providerLabels: Record<string, string> = {
  deepseek: "DeepSeek",
  zhipu: "Zhipu AI",
  qwen: "Alibaba Cloud · Qwen",
  mimo: "Xiaomi MiMo",
};

function providerLabel(provider: string) {
  return providerLabels[provider] ?? provider;
}

export function ModelSettingsEditor({
  classifierDraft,
  jobs = [],
  models,
  onClassifierChange,
  onProfileChange,
  onTest,
  profileDraft,
  profileModels,
}: {
  classifierDraft: ClassifierModelsDraft;
  jobs?: JobInfo[];
  models: ClassifierModelsResponse;
  onClassifierChange: (draft: ClassifierModelsDraft) => void;
  onProfileChange: (draft: ProfileModelsDraft) => void;
  onTest: (modelID: string, apiKey?: string) => Promise<JobInfo>;
  profileDraft: ProfileModelsDraft;
  profileModels: ProfileModelsResponse;
}) {
  const [testingModelID, setTestingModelID] = React.useState<string | null>(null);
  const [testError, setTestError] = React.useState<string | null>(null);
  const [testJobs, setTestJobs] = React.useState<Record<string, JobInfo>>({});
  const enabledSet = new Set(classifierDraft.enabledModelIds);
  const selectedModels = models.models.filter((model) => enabledSet.has(model.id));
  const configuredProfileIDs = new Set(profileModels.models.filter((model) => model.configured).map((model) => model.id));
  const profileOptions = profileModels.models.filter((model) => configuredProfileIDs.has(model.id) || enabledSet.has(model.id) || model.id === profileDraft.defaultModelId);
  const providerGroups = Array.from(models.models.reduce((groups, model) => {
    const group = groups.get(model.provider) ?? [];
    group.push(model);
    groups.set(model.provider, group);
    return groups;
  }, new Map<string, typeof models.models>()).entries()).map(([provider, providerModels]) => ({
    provider,
    label: providerLabel(provider),
    models: providerModels,
    configured: providerModels.some((model) => model.configured),
    environmentOverride: providerModels.some((model) => model.environment_override),
    requiresKey: providerModels.some((model) => !model.key_optional),
  }));

  const updateProviderKey = (provider: string, nextValue: string) => {
    const credentials = {...classifierDraft.credentials};
    if (nextValue.trim()) credentials[provider] = {value: nextValue};
    else delete credentials[provider];

    const enabledModelIds = enabledClassifierModelIdsAfterKeyChange(
      classifierDraft, models, provider, Boolean(credentials[provider]?.value?.trim()),
    );
    const defaultModelId = enabledModelIds.includes(classifierDraft.defaultModelId)
      ? classifierDraft.defaultModelId
      : enabledModelIds[0] ?? "";
    onClassifierChange({...classifierDraft, credentials, enabledModelIds, defaultModelId});

    const nextProfileOptions = profileModels.models.filter((model) =>
      model.configured || enabledModelIds.includes(model.id) || model.id === profileDraft.defaultModelId,
    );
    const profileDefaultModelId = nextProfileOptions.some((model) => model.id === profileDraft.defaultModelId)
      ? profileDraft.defaultModelId
      : nextProfileOptions[0]?.id ?? "";
    if (profileDefaultModelId !== profileDraft.defaultModelId) {
      onProfileChange({...profileDraft, defaultModelId: profileDefaultModelId});
    }
  };

  const handleTest = async (provider: string, modelID: string) => {
    setTestingModelID(modelID);
    setTestError(null);
    try {
      const key = classifierDraft.credentials[provider]?.value ?? "";
      const job = await onTest(modelID, key);
      setTestJobs((current) => ({...current, [modelID]: job}));
    } catch (error) {
      setTestError(error instanceof Error ? error.message : "Connection test could not be started.");
    } finally {
      setTestingModelID(null);
    }
  };

  return (
    <div className="space-y-5">
      <div className="space-y-4">
        <SelectField
          disabled={selectedModels.length === 0}
          id="classifier-default-model"
          label="Default classifier"
          options={selectedModels.map((model) => ({label: model.label, value: model.id}))}
          value={classifierDraft.defaultModelId}
          onChange={(defaultModelId) => onClassifierChange({...classifierDraft, defaultModelId})}
        />
        {selectedModels.length === 0 ? <p className="text-sm text-danger">Add a provider API key to choose a default classifier.</p> : null}

        <SelectField
          disabled={profileOptions.length === 0}
          id="profile-default-model"
          label="Default profile model"
          options={profileOptions.map((model) => ({label: model.label, value: model.id}))}
          value={profileOptions.some((model) => model.id === profileDraft.defaultModelId)
            ? profileDraft.defaultModelId
            : profileOptions[0]?.id ?? ""}
          onChange={(defaultModelId) => onProfileChange({...profileDraft, defaultModelId})}
        />
      </div>

      <section>
        <h3 className="mb-2 text-sm font-semibold text-(--ink)">API keys by provider</h3>
        <div className="divide-y divide-(--line) border-y border-(--line)">
          {providerGroups.map((group) => {
            const selectedTestModel = group.models.find((model) => model.id === classifierDraft.defaultModelId)
              ?? group.models.find((model) => model.id === profileDraft.defaultModelId)
              ?? group.models[0];
            const testJob = testStateForModel(jobs, selectedTestModel.id) ?? testJobs[selectedTestModel.id] ?? null;
            const testing = testingModelID === selectedTestModel.id || testJob?.status === "queued" || testJob?.status === "running";
            const testLabel = testing
              ? "Testing..."
              : testJob?.status === "completed"
                ? "Connected"
                : testJob?.status === "failed"
                  ? "Test failed"
                  : "Test connection";

            return (
              <div key={group.provider} className="space-y-3 py-4">
                <div>
                  <p className="text-sm font-medium text-(--ink)">{group.label}</p>
                  <p className="mt-1 text-xs leading-5 text-muted">{group.models.map((model) => model.label).join(" · ")}</p>
                </div>
                <div className="grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-start">
                  <div className="min-w-0">
                    {group.requiresKey ? (
                      <TextInputField
                        disabled={group.environmentOverride}
                        label={`${group.label} API key`}
                        description={group.environmentOverride ? "Managed by the environment; change the key in its source configuration." : undefined}
                        placeholder={group.environmentOverride
                          ? "Managed by environment"
                          : group.configured
                            ? "Leave blank to keep the current key"
                            : "Paste API key"}
                        type="password"
                        value={classifierDraft.credentials[group.provider]?.value ?? ""}
                        onChange={(value) => updateProviderKey(group.provider, value)}
                      />
                    ) : (
                      <p className="text-sm leading-5 text-muted">Built-in model; no API key needed.</p>
                    )}
                  </div>
                  <Button
                    aria-label={`Test ${group.label} connection using ${selectedTestModel.label}`}
                    className="sm:mt-7"
                    isDisabled={!group.configured && !classifierDraft.credentials[group.provider]?.value?.trim() || testing}
                    size="sm"
                    variant={testJob?.status === "completed" ? "secondary" : "outline"}
                    onPress={() => void handleTest(group.provider, selectedTestModel.id)}
                  >
                    {testLabel}
                  </Button>
                </div>
              </div>
            );
          })}
        </div>
      </section>
      {testError ? <StatusBanner tone="danger">{testError}</StatusBanner> : null}
    </div>
  );
}
