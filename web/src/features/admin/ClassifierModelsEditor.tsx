import {Button} from "@heroui/react";
import React from "react";

import {TextInputField, CheckboxRow} from "../../shared/components/FormFields";
import {SelectField} from "../../shared/components/SelectField";
import {StatusBanner} from "../../shared/components/StatusBanner";
import type {ClassifierModelsResponse, JobInfo, SettingsConfigUpdate} from "../../shared/types";

export type ClassifierModelsDraft = {
  enabledModelIds: string[];
  defaultModelId: string;
  credentials: Record<string, SettingsConfigUpdate>;
  reuseDeepSeekKeyForProfile: boolean;
};

export function createClassifierModelsDraft(response: ClassifierModelsResponse): ClassifierModelsDraft {
  const selected = response.models.filter((model) => model.configured).map((model) => model.id);
  return {
    enabledModelIds: selected,
    defaultModelId: selected.includes(response.default_model_id) ? response.default_model_id : selected[0] ?? "",
    credentials: {},
    reuseDeepSeekKeyForProfile: false,
  };
}

export function classifierModelsUpdateFromDraft(draft: ClassifierModelsDraft) {
  return {
    enabled_model_ids: draft.enabledModelIds,
    default_model_id: draft.defaultModelId,
    credentials: draft.credentials,
    reuse_deepseek_key_for_profile: draft.reuseDeepSeekKeyForProfile,
  };
}

export function classifierModelsDraftHasRequiredKeys(
  draft: ClassifierModelsDraft,
  models: ClassifierModelsResponse,
): boolean {
  return draft.enabledModelIds.length > 0 && draft.enabledModelIds.every((modelID) => {
    const model = models.models.find((item) => item.id === modelID);
    const credential = draft.credentials[modelID];
    if (credential?.value?.trim()) return true;
    if (!model?.configured) return false;
    return !credential?.clear || model.environment_override;
  });
}

function testStateForJob(jobs: JobInfo[], modelID: string): JobInfo | null {
  return jobs.find((job) => {
    if (job.job_type !== "model-test") return false;
    const resultModel = typeof job.result?.model_id === "string" ? job.result.model_id : "";
    return resultModel === modelID || job.message?.includes(modelID) === true;
  }) ?? null;
}

export function ClassifierModelsEditor({
  draft,
  jobs = [],
  models,
  onChange,
  onTest,
  profileConfigured = false,
  showReuse = false,
}: {
  draft: ClassifierModelsDraft;
  jobs?: JobInfo[];
  models: ClassifierModelsResponse;
  onChange: (draft: ClassifierModelsDraft) => void;
  onTest: (modelID: string, apiKey?: string) => Promise<JobInfo>;
  profileConfigured?: boolean;
  showReuse?: boolean;
}) {
  const [testingModelID, setTestingModelID] = React.useState<string | null>(null);
  const [testError, setTestError] = React.useState<string | null>(null);
  const [testJobs, setTestJobs] = React.useState<Record<string, JobInfo>>({});
  const reuseDeepSeekTouchedRef = React.useRef(false);
  const enabledSet = new Set(draft.enabledModelIds);
  const selectedModels = models.models.filter((model) => enabledSet.has(model.id));

  React.useEffect(() => {
    reuseDeepSeekTouchedRef.current = false;
  }, [models, profileConfigured]);

  const updateCredential = (modelID: string, nextValue: string) => {
    const credentials = {...draft.credentials};
    const previousValue = credentials[modelID]?.value?.trim() ?? "";
    if (nextValue.trim()) credentials[modelID] = {value: nextValue};
    else delete credentials[modelID];
    const enabledModelIds = models.models
      .filter((model) => model.configured || Boolean(credentials[model.id]?.value?.trim()))
      .map((model) => model.id);
    const defaultModelId = enabledModelIds.includes(draft.defaultModelId)
      ? draft.defaultModelId
      : enabledModelIds[0] ?? "";
    let reuseDeepSeekKeyForProfile = draft.reuseDeepSeekKeyForProfile;
    const deepSeek = models.models.find((model) => model.id === "deepseek-v4-flash");
    if (modelID === "deepseek-v4-flash" && !profileConfigured && enabledModelIds.includes(modelID)) {
      // Once a newly entered DeepSeek key becomes available, opt into the
      // one-time Profile reuse by default. Removing an unsaved key disables
      // the option again; an explicit user uncheck remains respected while
      // editing an existing value.
      if (nextValue.trim() && !previousValue && !deepSeek?.configured && !reuseDeepSeekTouchedRef.current) reuseDeepSeekKeyForProfile = true;
      if (!nextValue.trim() && !deepSeek?.configured) reuseDeepSeekKeyForProfile = false;
    }
    onChange({...draft, credentials, enabledModelIds, defaultModelId, reuseDeepSeekKeyForProfile});
  };

  const handleTest = async (modelID: string) => {
    setTestingModelID(modelID);
    setTestError(null);
    try {
      const key = draft.credentials[modelID]?.value ?? "";
      const job = await onTest(modelID, key);
      setTestJobs((current) => ({...current, [modelID]: job}));
    } catch (error) {
      setTestError(error instanceof Error ? error.message : "Connection test could not be started.");
    } finally {
      setTestingModelID(null);
    }
  };

  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <SelectField
          disabled={selectedModels.length === 0}
          id="classifier-default-model"
          label="Default classifier"
          options={selectedModels.map((model) => ({label: model.label, value: model.id}))}
          value={draft.defaultModelId}
          onChange={(defaultModelId) => onChange({...draft, defaultModelId})}
        />
        {selectedModels.length === 0 ? <p className="text-sm text-danger">Add at least one API key to choose a default classifier.</p> : null}
      </div>

      <div className="divide-y divide-(--line) border-y border-(--line)">
        {models.models.map((model) => {
          const credential = draft.credentials[model.id];
          const testJob = testStateForJob(jobs, model.id) ?? testJobs[model.id] ?? null;
          const testLabel = testingModelID === model.id
            ? "Testing..."
            : testJob?.status === "completed"
              ? "Connected"
              : testJob?.status === "failed"
                ? "Test failed"
                : "Test connection";
          return (
            <div key={model.id} className="grid gap-3 py-3 sm:grid-cols-[minmax(150px,0.32fr)_minmax(260px,1fr)_auto] sm:items-center">
              <p className="text-sm font-medium text-(--ink)">{model.label}</p>
              <div className="min-w-0">
                  <TextInputField
                    hideLabel
                    label={`${model.label} API key`}
                    placeholder={model.configured ? "Leave blank to keep the current key" : "Paste API key"}
                    type="password"
                    value={typeof credential?.value === "string" ? credential.value : ""}
                    onChange={(value) => updateCredential(model.id, value)}
                  />
              </div>
              <Button
                isDisabled={testingModelID === model.id}
                size="sm"
                variant={testJob?.status === "completed" ? "secondary" : "outline"}
                onPress={() => void handleTest(model.id)}
              >
                {testLabel}
              </Button>
            </div>
          );
        })}
      </div>

      {showReuse ? (
        <CheckboxRow
          checked={draft.reuseDeepSeekKeyForProfile}
          disabled={!models.models.some((model) => model.id === "deepseek-v4-flash" && enabledSet.has(model.id) && (model.configured || Boolean(draft.credentials[model.id]?.value))) || profileConfigured}
          onChange={(checked) => {
            reuseDeepSeekTouchedRef.current = true;
            onChange({...draft, reuseDeepSeekKeyForProfile: checked});
          }}
        >
          <span className="space-y-1">
            <span className="block text-sm font-medium text-(--ink)">Reuse the DeepSeek classification key for Profile generation once</span>
            <span className="block text-xs leading-5 text-muted">Only fills an empty Profile key; an existing Profile key is never overwritten.</span>
          </span>
        </CheckboxRow>
      ) : null}
      {testError ? <StatusBanner tone="danger">{testError}</StatusBanner> : null}
    </div>
  );
}
