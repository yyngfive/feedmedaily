import {Button} from "@heroui/react";
import React from "react";

import {SelectField} from "../../shared/components/SelectField";
import {StatusBanner} from "../../shared/components/StatusBanner";
import type {JobInfo, ProfileModelsResponse, SettingsConfigField} from "../../shared/types";

export type ProfileModelsDraft = {
  defaultModelId: string;
};

export function createProfileModelsDraft(response: ProfileModelsResponse): ProfileModelsDraft {
  const configured = response.models.filter((model) => model.configured).map((model) => model.id);
  return {
    defaultModelId: configured.includes(response.default_model_id) ? response.default_model_id : configured[0] ?? "",
  };
}

function testStateForJob(jobs: JobInfo[], modelID: string): JobInfo | null {
  return jobs.find((job) => {
    if (job.job_type !== "profile-model-test") return false;
    const resultModel = typeof job.result?.model_id === "string" ? job.result.model_id : "";
    return resultModel === modelID || job.message?.includes(modelID) === true;
  }) ?? null;
}

export function ProfileModelsEditor({
  draft,
  jobs = [],
  models,
  onChange,
  onTest,
}: {
  draft: ProfileModelsDraft;
  jobs?: JobInfo[];
  models: ProfileModelsResponse;
  onChange: (draft: ProfileModelsDraft) => void;
  onTest: (modelID: string) => Promise<JobInfo>;
}) {
  const [testingModelID, setTestingModelID] = React.useState<string | null>(null);
  const [testError, setTestError] = React.useState<string | null>(null);
  const [testJobs, setTestJobs] = React.useState<Record<string, JobInfo>>({});
  const configuredModels = models.models.filter((model) => model.configured);
  const configuredIDs = configuredModels.map((model) => model.id);

  const handleTest = async (modelID: string) => {
    setTestingModelID(modelID);
    setTestError(null);
    try {
      const job = await onTest(modelID);
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
          disabled={configuredModels.length === 0}
          id="profile-default-model"
          label="Default profile model"
          options={configuredModels.map((model) => ({label: model.label, value: model.id}))}
          value={configuredIDs.includes(draft.defaultModelId) ? draft.defaultModelId : configuredIDs[0] ?? ""}
          onChange={(defaultModelId) => onChange({...draft, defaultModelId})}
          description="Every profile model reuses the API key of the matching classifier provider."
        />
        {configuredModels.length === 0 ? <p className="text-sm text-danger">Add a classifier provider key first to choose a profile model.</p> : null}
      </div>

      <div className="divide-y divide-(--line) border-y border-(--line)">
        {models.models.map((model) => {
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
              <p className="text-sm leading-5 text-muted">
                {model.configured ? `Uses the shared ${model.provider} classifier key.` : "No shared classifier key configured yet."}
              </p>
              <Button
                isDisabled={!model.configured || testingModelID === model.id}
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
      {testError ? <StatusBanner tone="danger">{testError}</StatusBanner> : null}
    </div>
  );
}
