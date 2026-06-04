import {Button, Card, Spinner} from "@heroui/react";
import React from "react";

import {statusMessage} from "../../app/utils";
import {
  applyProfileProposal,
  bootstrapProfile,
  fetchCurrentProfile,
  fetchJobs,
  fetchProfileProposals,
  fetchSettingsConfig,
  rejectProfileProposal,
  saveCurrentProfile,
  saveSettingsConfig,
} from "../../reportData";
import type {
  ClassificationProfile,
  JobInfo,
  ProfileProposal,
  SettingsConfigField,
  SettingsConfigUpdate,
} from "../../types";
import {StatusBanner, type StatusTone} from "../common/StatusBanner";

const aiAdvancedKeys = [
  "SCIRSS_CLASSIFIER_API_KEY",
  "SCIRSS_CLASSIFIER_BASE_URL",
  "SCIRSS_CLASSIFIER_MODEL",
  "SCIRSS_CLASSIFIER_THINKING",
  "SCIRSS_CLASSIFIER_BATCH_SIZE",
  "SCIRSS_PROFILE_API_KEY",
  "SCIRSS_PROFILE_BASE_URL",
  "SCIRSS_PROFILE_MODEL",
  "SCIRSS_PROFILE_THINKING",
];

const zoteroKeys = [
  "SCIRSS_ZOTERO_API_KEY",
  "SCIRSS_ZOTERO_LIBRARY_TYPE",
  "SCIRSS_ZOTERO_LIBRARY_ID",
  "SCIRSS_ZOTERO_COLLECTION_KEY",
];

const localAppKeys = ["SCIRSS_PROFILE_PATH", "SCIRSS_SERVER_HOST", "SCIRSS_SERVER_PORT"];

type ProfileDraft = {
  name: string;
  scope: string;
  directRulesText: string;
  indirectRulesText: string;
  unrelatedRulesText: string;
};

function errorText(error: unknown, fallback: string): string {
  return error instanceof Error && error.message.trim() ? error.message : fallback;
}

function createInitialFieldValues(fields: SettingsConfigField[]): Record<string, string> {
  return Object.fromEntries(
    fields.map((field) => {
      if (field.secret) {
        return [field.key, ""];
      }
      if (field.value != null) {
        return [field.key, field.value];
      }
      if (field.default_value != null) {
        return [field.key, field.default_value];
      }
      return [field.key, field.options[0]?.value ?? ""];
    }),
  );
}

function fieldsForKeys(fields: SettingsConfigField[], keys: string[]) {
  const fieldMap = new Map(fields.map((field) => [field.key, field]));
  return keys.flatMap((key) => {
    const field = fieldMap.get(key);
    return field ? [field] : [];
  });
}

function isOverrideKey(fieldKey: string) {
  return fieldKey === "SCIRSS_CLASSIFIER_API_KEY" || fieldKey === "SCIRSS_PROFILE_API_KEY";
}

function createDraft(profile: ClassificationProfile): ProfileDraft {
  return {
    name: profile.meta.name,
    scope: profile.scope,
    directRulesText: profile.relevance_rules.direct.join("\n"),
    indirectRulesText: profile.relevance_rules.indirect.join("\n"),
    unrelatedRulesText: profile.relevance_rules.unrelated.join("\n"),
  };
}

function parseRules(text: string): string[] {
  return text
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function draftToProfile(profile: ClassificationProfile, draft: ProfileDraft): ClassificationProfile {
  return {
    ...profile,
    meta: {
      ...profile.meta,
      name: draft.name.trim() || profile.meta.name,
    },
    scope: draft.scope.trim(),
    relevance_rules: {
      direct: parseRules(draft.directRulesText),
      indirect: parseRules(draft.indirectRulesText),
      unrelated: parseRules(draft.unrelatedRulesText),
    },
  };
}

function buildSettingsPayload(
  fields: SettingsConfigField[],
  values: Record<string, string>,
  sharedApiKey: string,
): Record<string, SettingsConfigUpdate> {
  const payload: Record<string, SettingsConfigUpdate> = {};
  const shared = sharedApiKey.trim();

  fields.forEach((field) => {
    const rawValue = values[field.key] ?? "";
    const trimmedValue = rawValue.trim();

    if (field.secret) {
      if (field.key === "SCIRSS_CLASSIFIER_API_KEY") {
        if (trimmedValue) {
          payload[field.key] = {value: trimmedValue};
        } else if (shared) {
          payload[field.key] = {value: shared};
        }
        return;
      }
      if (field.key === "SCIRSS_PROFILE_API_KEY") {
        if (trimmedValue) {
          payload[field.key] = {value: trimmedValue};
        } else if (shared) {
          payload[field.key] = {value: shared};
        }
        return;
      }
      if (trimmedValue) {
        payload[field.key] = {value: trimmedValue};
      }
      return;
    }

    payload[field.key] = {value: trimmedValue};
  });

  return payload;
}

function mergeJobs(current: JobInfo[], nextJob: JobInfo): JobInfo[] {
  const byId = new Map(current.map((job) => [job.id, job]));
  byId.set(nextJob.id, nextJob);
  return Array.from(byId.values()).sort((left, right) =>
    left.created_at < right.created_at ? 1 : -1,
  );
}

function sharedKeySummary(
  classifierField: SettingsConfigField | undefined,
  profileField: SettingsConfigField | undefined,
): string | null {
  const configuredClassifier = Boolean(classifierField?.configured);
  const configuredProfile = Boolean(profileField?.configured);
  if (!configuredClassifier && !configuredProfile) {
    return null;
  }

  if (configuredClassifier && configuredProfile) {
    return "Leave blank to keep the current model keys.";
  }

  if (configuredClassifier) {
    return "Leave blank to keep the current classifier key.";
  }

  return "Leave blank to keep the current profile key.";
}

function SettingFieldRow({
  field,
  value,
  onChange,
}: {
  field: SettingsConfigField;
  value: string;
  onChange: (value: string) => void;
}) {
  const placeholder = field.secret
    ? field.configured
      ? "Leave blank to keep the current secret"
      : isOverrideKey(field.key)
        ? "Leave blank to use the shared API key"
        : "Paste your API key"
    : field.default_value ?? "";

  return (
    <label className="grid gap-4 py-2 md:grid-cols-[minmax(0,0.7fr)_minmax(280px,0.8fr)] md:items-center">
      <div className="space-y-1">
        <span className="text-sm font-medium text-(--ink)">{field.label}</span>
      </div>

      <div className="min-w-0">
        {field.input_type === "select" ? (
          <select
            aria-label={field.label}
            className="w-full rounded-md border border-(--line) bg-(--paper-accent) px-3 py-2 text-sm text-(--ink)"
            value={value}
            onChange={(event) => onChange(event.target.value)}
          >
            {field.options.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        ) : (
          <input
            aria-label={field.label}
            className="w-full rounded-md border border-(--line) bg-(--paper-accent) px-3 py-2 text-sm text-(--ink) placeholder:text-muted"
            inputMode={field.input_type === "number" ? "numeric" : undefined}
            placeholder={placeholder}
            type={
              field.secret
                ? "password"
                : field.input_type === "number"
                  ? "number"
                  : field.input_type === "url"
                    ? "url"
                    : "text"
            }
            value={value}
            onChange={(event) => onChange(event.target.value)}
          />
        )}
      </div>
    </label>
  );
}

function AdvancedGroup({
  fields,
  onChange,
  title,
  values,
}: {
  fields: SettingsConfigField[];
  onChange: (key: string, value: string) => void;
  title: string;
  values: Record<string, string>;
}) {
  return (
    <section className="space-y-2 p-2">
      <div className="space-y-1 py-2">
        <h2 className="text-base font-bold text-(--ink)">{title}</h2>
      </div>
      <div>
        {fields.map((field) => (
          <SettingFieldRow
            key={field.key}
            field={field}
            value={values[field.key] ?? ""}
            onChange={(nextValue) => onChange(field.key, nextValue)}
          />
        ))}
      </div>
    </section>
  );
}

function DraftField({
  children,
  hint,
  title,
}: {
  children: React.ReactNode;
  hint?: string;
  title: string;
}) {
  return (
    <label className="block space-y-2">
      <span className="text-sm font-semibold text-(--ink)">{title}</span>
      {hint ? <p className="text-sm leading-6 text-muted">{hint}</p> : null}
      {children}
    </label>
  );
}

function RuleTextareaSection({
  hint = "One rule per line.",
  onChange,
  text,
  title,
}: {
  hint?: string;
  onChange: (value: string) => void;
  text: string;
  title: string;
}) {
  return (
    <section className="space-y-2">
      <div className="min-w-0 space-y-1">
        <h3 className="text-sm font-semibold text-(--ink)">{title}</h3>
        <p className="text-sm text-muted">{hint}</p>
      </div>
      <textarea
        aria-label={title}
        className="min-h-24 w-full resize-y rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm leading-6 text-(--ink)"
        value={text}
        onChange={(event) => onChange(event.target.value)}
      />
    </section>
  );
}

function ProposalDraftCard({
  accepting,
  draft,
  message,
  onAccept,
  onChangeDraft,
  onReject,
  pendingProposal,
  rejecting,
}: {
  accepting: boolean;
  draft: ProfileDraft | null;
  message: {text: string; tone: StatusTone} | null;
  onAccept: () => void;
  onChangeDraft: React.Dispatch<React.SetStateAction<ProfileDraft | null>>;
  onReject: () => void;
  pendingProposal: ProfileProposal | null;
  rejecting: boolean;
}) {
  return (
    <Card className="border border-(--line) bg-(--paper-accent)">
      <Card.Header className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <p className="text-sm font-semibold text-(--ink)">Pending initial profile proposal</p>
          <h2 className="text-xl font-semibold text-(--ink)">
            {pendingProposal ? `${pendingProposal.proposed_profile.meta.name} · v${pendingProposal.proposed_profile.meta.version}` : "Waiting for proposal"}
          </h2>
          <p className="text-sm leading-6 text-muted">
            {pendingProposal
              ? `${pendingProposal.created_at.slice(0, 10)} · ${pendingProposal.summary}`
              : "Generate an initial profile to review and edit it here before applying it."}
          </p>
        </div>
        {pendingProposal ? (
          <div className="flex flex-wrap gap-2">
            <Button isDisabled={accepting || rejecting || !draft} size="sm" onPress={onAccept}>
              {accepting ? "Accepting..." : "Accept"}
            </Button>
            <Button
              isDisabled={accepting || rejecting}
              size="sm"
              variant="ghost"
              onPress={onReject}
            >
              {rejecting ? "Rejecting..." : "Reject"}
            </Button>
          </div>
        ) : null}
      </Card.Header>
      <Card.Content className="space-y-4">
        {message ? <StatusBanner tone={message.tone}>{message.text}</StatusBanner> : null}

        {!pendingProposal ? (
          <p className="text-sm leading-6 text-muted">
            No pending proposal yet. Once the bootstrap job finishes, the connected demo will load
            the real proposal here.
          </p>
        ) : !draft ? (
          <div className="flex items-center gap-2 text-sm text-muted">
            <Spinner color="current" size="sm" />
            Preparing the editable draft...
          </div>
        ) : (
          <>
            <DraftField title="Profile name">
              <input
                className="w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm text-(--ink)"
                value={draft.name}
                onChange={(event) =>
                  onChangeDraft((current) =>
                    current ? {...current, name: event.target.value} : current,
                  )
                }
              />
            </DraftField>

            <DraftField
              title="Scope"
              hint="Describe the research boundary and what should count as a strong match."
            >
              <textarea
                className="min-h-24 w-full rounded-md border border-(--line) bg-(--paper) px-3 py-2 text-sm leading-6 text-(--ink)"
                value={draft.scope}
                onChange={(event) =>
                  onChangeDraft((current) =>
                    current ? {...current, scope: event.target.value} : current,
                  )
                }
              />
            </DraftField>

            <div className="space-y-4">
              <RuleTextareaSection
                title="Direct rules"
                text={draft.directRulesText}
                onChange={(value) =>
                  onChangeDraft((current) => (current ? {...current, directRulesText: value} : current))
                }
              />

              <RuleTextareaSection
                title="Indirect rules"
                text={draft.indirectRulesText}
                onChange={(value) =>
                  onChangeDraft((current) =>
                    current ? {...current, indirectRulesText: value} : current,
                  )
                }
              />

              <RuleTextareaSection
                title="Unrelated rules"
                text={draft.unrelatedRulesText}
                onChange={(value) =>
                  onChangeDraft((current) =>
                    current ? {...current, unrelatedRulesText: value} : current,
                  )
                }
              />
            </div>
          </>
        )}
      </Card.Content>
    </Card>
  );
}

export function OnboardingSettingsDemo() {
  const [loading, setLoading] = React.useState(true);
  const [interestDescription, setInterestDescription] = React.useState("");
  const [sharedApiKey, setSharedApiKey] = React.useState("");
  const [advancedOpen, setAdvancedOpen] = React.useState(false);
  const [advancedValues, setAdvancedValues] = React.useState<Record<string, string>>({});
  const [settingsFields, setSettingsFields] = React.useState<SettingsConfigField[]>([]);
  const [jobs, setJobs] = React.useState<JobInfo[]>([]);
  const [proposals, setProposals] = React.useState<ProfileProposal[]>([]);
  const [currentProfile, setCurrentProfile] = React.useState<ClassificationProfile | null>(null);
  const [settingsBusy, setSettingsBusy] = React.useState(false);
  const [accepting, setAccepting] = React.useState(false);
  const [rejecting, setRejecting] = React.useState(false);
  const [pageMessage, setPageMessage] = React.useState<{text: string; tone: StatusTone} | null>(null);
  const [settingsMessage, setSettingsMessage] = React.useState<{
    text: string;
    tone: StatusTone;
  } | null>(null);
  const [proposalMessage, setProposalMessage] = React.useState<{
    text: string;
    tone: StatusTone;
  } | null>(null);
  const [profileDraft, setProfileDraft] = React.useState<ProfileDraft | null>(null);

  const pendingProposal = React.useMemo(
    () => proposals.find((item) => item.state === "pending") ?? null,
    [proposals],
  );
  const latestBootstrapJob = React.useMemo(
    () => jobs.find((item) => item.job_type === "profile-bootstrap") ?? null,
    [jobs],
  );
  const bootstrapRunning =
    latestBootstrapJob?.status === "queued" || latestBootstrapJob?.status === "running";
  const proposalIdentityRef = React.useRef<number | null>(null);

  const aiAdvancedFields = React.useMemo(
    () => fieldsForKeys(settingsFields, aiAdvancedKeys),
    [settingsFields],
  );
  const classifierApiKeyField = React.useMemo(
    () => settingsFields.find((field) => field.key === "SCIRSS_CLASSIFIER_API_KEY"),
    [settingsFields],
  );
  const profileApiKeyField = React.useMemo(
    () => settingsFields.find((field) => field.key === "SCIRSS_PROFILE_API_KEY"),
    [settingsFields],
  );
  const sharedKeyHint = React.useMemo(
    () => sharedKeySummary(classifierApiKeyField, profileApiKeyField),
    [classifierApiKeyField, profileApiKeyField],
  );
  const zoteroFields = React.useMemo(
    () => fieldsForKeys(settingsFields, zoteroKeys),
    [settingsFields],
  );
  const localAppFields = React.useMemo(
    () => fieldsForKeys(settingsFields, localAppKeys),
    [settingsFields],
  );
  const editableSettingsFields = React.useMemo(() => {
    const seen = new Set<string>();
    return [...aiAdvancedFields, ...zoteroFields, ...localAppFields].filter((field) => {
      if (seen.has(field.key)) {
        return false;
      }
      seen.add(field.key);
      return true;
    });
  }, [aiAdvancedFields, localAppFields, zoteroFields]);

  const refreshConnectedState = React.useCallback(async () => {
    const [proposalItems, currentProfileResponse, jobItems] = await Promise.all([
      fetchProfileProposals(),
      fetchCurrentProfile(),
      fetchJobs(),
    ]);
    setProposals(proposalItems);
    setCurrentProfile(currentProfileResponse.profile);
    setJobs(jobItems);
  }, []);

  const refreshSettingsState = React.useCallback(async () => {
    const settingsResponse = await fetchSettingsConfig();
    setSettingsFields(settingsResponse.fields);
    setAdvancedValues(createInitialFieldValues(settingsResponse.fields));
  }, []);

  React.useEffect(() => {
    let cancelled = false;

    const load = async () => {
      try {
        const [settingsResponse, proposalItems, currentProfileResponse, jobItems] = await Promise.all([
          fetchSettingsConfig(),
          fetchProfileProposals(),
          fetchCurrentProfile(),
          fetchJobs(),
        ]);
        if (cancelled) {
          return;
        }
        setSettingsFields(settingsResponse.fields);
        setAdvancedValues(createInitialFieldValues(settingsResponse.fields));
        setProposals(proposalItems);
        setCurrentProfile(currentProfileResponse.profile);
        setJobs(jobItems);
        setPageMessage(null);
      } catch (error) {
        if (!cancelled) {
          setPageMessage({
            tone: "danger",
            text: errorText(error, "Could not load connected onboarding data."),
          });
        }
      } finally {
        if (!cancelled) {
          setLoading(false);
        }
      }
    };

    void load();
    return () => {
      cancelled = true;
    };
  }, []);

  React.useEffect(() => {
    if (loading) {
      return;
    }

    let cancelled = false;
    const timer = window.setInterval(() => {
      void (async () => {
        try {
          const [proposalItems, currentProfileResponse, jobItems] = await Promise.all([
            fetchProfileProposals(),
            fetchCurrentProfile(),
            fetchJobs(),
          ]);
          if (cancelled) {
            return;
          }
          setProposals(proposalItems);
          setCurrentProfile(currentProfileResponse.profile);
          setJobs(jobItems);
        } catch {
          // Keep the current page stable if a background poll fails.
        }
      })();
    }, 2500);

    return () => {
      cancelled = true;
      window.clearInterval(timer);
    };
  }, [loading]);

  React.useEffect(() => {
    const currentPendingProposal = pendingProposal;
    if (!currentPendingProposal) {
      proposalIdentityRef.current = null;
      setProfileDraft(null);
      return;
    }
    const nextProposalId = currentPendingProposal.id;
    if (proposalIdentityRef.current === nextProposalId) {
      return;
    }
    proposalIdentityRef.current = nextProposalId;
    setProfileDraft(createDraft(currentPendingProposal.proposed_profile));
  }, [pendingProposal]);

  const handleAdvancedValueChange = (key: string, value: string) => {
    setAdvancedValues((current) => ({...current, [key]: value}));
  };

  const handleSaveAndGenerate = async () => {
    if (!interestDescription.trim() || currentProfile) {
      return;
    }

    setSettingsBusy(true);
    setSettingsMessage(null);
    setProposalMessage(null);

    try {
      const saved = await saveSettingsConfig(
        buildSettingsPayload(editableSettingsFields, advancedValues, sharedApiKey),
      );
      setSettingsFields(saved.fields);
      setAdvancedValues(createInitialFieldValues(saved.fields));
      setSharedApiKey("");
    } catch (error) {
      setSettingsBusy(false);
      setSettingsMessage({
        tone: "danger",
        text: errorText(error, "Could not save local settings."),
      });
      return;
    }

    try {
      const job = await bootstrapProfile({interest_description: interestDescription.trim()});
      setJobs((current) => mergeJobs(current, job));
      setSettingsMessage({
        tone: "info",
        text: "Local settings saved. Initial profile generation started.",
      });
    } catch (error) {
      setSettingsMessage({
        tone: "warning",
        text: `Local settings were saved, but the initial profile generation did not start: ${errorText(error, "Unknown error.")}`,
      });
    } finally {
      setSettingsBusy(false);
    }
  };

  const handleAcceptDraft = async () => {
    if (!pendingProposal || !profileDraft) {
      return;
    }

    setAccepting(true);
    setProposalMessage(null);

    try {
      const appliedProposal = await applyProfileProposal(pendingProposal.id);
      const baseProfile =
        appliedProposal.applied_profile ?? (await fetchCurrentProfile()).profile;

      if (!baseProfile) {
        throw new Error("The proposal was applied, but no current profile could be loaded afterward.");
      }

      const editedProfile = draftToProfile(baseProfile, profileDraft);

      try {
        const savedProfile = await saveCurrentProfile(editedProfile);
        setCurrentProfile(savedProfile.profile);
        await refreshConnectedState();
        const nextUrl = new URL(window.location.href);
        nextUrl.searchParams.delete("demo");
        window.location.href = `${nextUrl.pathname}${nextUrl.search}${nextUrl.hash}`;
      } catch (error) {
        await refreshConnectedState();
        setProposalMessage({
          tone: "warning",
          text: `The original proposal was applied, but the edited draft was not fully saved: ${errorText(
            error,
            "Unknown error.",
          )}`,
        });
      }
    } catch (error) {
      setProposalMessage({
        tone: "danger",
        text: errorText(error, "Could not apply the profile proposal."),
      });
    } finally {
      setAccepting(false);
    }
  };

  const handleRejectProposal = async () => {
    if (!pendingProposal) {
      return;
    }

    setRejecting(true);
    setProposalMessage(null);

    try {
      await rejectProfileProposal(pendingProposal.id);
      await refreshConnectedState();
      setProposalMessage({
        tone: "success",
        text: "Rejected the pending proposal.",
      });
    } catch (error) {
      setProposalMessage({
        tone: "danger",
        text: errorText(error, "Could not reject the profile proposal."),
      });
    } finally {
      setRejecting(false);
    }
  };

  if (loading) {
    return (
      <main className="mx-auto flex min-h-[50vh] max-w-7xl items-center justify-center px-4 py-6">
        <div className="rounded-lg border border-(--line) bg-(--paper-accent) px-5 py-4 text-sm text-muted">
          Loading connected onboarding demo...
        </div>
      </main>
    );
  }

  return (
    <main className="mx-auto max-w-7xl px-4 py-6">
      <div className="space-y-4">
        {pageMessage ? <StatusBanner tone={pageMessage.tone}>{pageMessage.text}</StatusBanner> : null}
        {currentProfile ? (
          <StatusBanner tone="warning">
            A classification profile already exists. This connected demo is meant for first-run
            onboarding, so Save and Generate is disabled.
          </StatusBanner>
        ) : null}
      </div>

      <div className="mt-4 grid gap-4 xl:grid-cols-[minmax(0,1fr)_520px]">
        <div className="space-y-4">
          <Card className="border border-(--line) bg-(--paper-accent)">
            <Card.Header className="space-y-2">
              <h1 className="text-2xl font-semibold text-(--ink)">Basic Settings</h1>
            </Card.Header>
            <Card.Content className="space-y-1">
              <label className="block p-2">
                <span className="text-sm font-medium text-(--ink)">Profile Prompt</span>
                <span className="mt-1 block text-sm leading-6 text-muted">
                  Describe the topics, methods, validation expectations, and paper types you want the profile to prioritize or avoid.
                </span>
                <textarea
                  className="mt-3 min-h-56 w-full rounded-md border border-(--line) bg-(--paper-accent) px-3 py-2 text-sm text-(--ink) placeholder:text-muted"
                  placeholder="For example: I care about AI-driven protein design, structure prediction, and experimental validation. Prioritize strong method papers and reproducible evaluation. Avoid broad reviews and speculative work without evidence."
                  value={interestDescription}
                  onChange={(event) => setInterestDescription(event.target.value)}
                />
              </label>
              <label className="block p-2">
                <span className="text-sm font-medium text-(--ink)">LLM API Key</span>
                <span className="mt-1 block text-sm leading-6 text-muted">
                  DeepSeek is the default provider. Get and fund your key at{" "}
                  <a
                    className="underline underline-offset-2"
                    href="https://platform.deepseek.com/"
                    rel="noopener noreferrer"
                    target="_blank"
                  >
                    platform.deepseek.com
                  </a>
                  .
                </span>
                <input
                  className="mt-3 w-full rounded-md border border-(--line) bg-(--paper-accent) px-3 py-2 text-sm text-(--ink) placeholder:text-muted"
                  placeholder={sharedKeyHint ? "Leave blank to keep the current model keys" : "sk-..."}
                  type="password"
                  value={sharedApiKey}
                  onChange={(event) => setSharedApiKey(event.target.value)}
                />
              </label>

              {latestBootstrapJob ? (
                <StatusBanner
                  tone={
                    latestBootstrapJob.status === "failed"
                      ? "danger"
                      : latestBootstrapJob.status === "completed"
                        ? "success"
                        : "info"
                  }
                >
                  <p className="font-medium">
                    {bootstrapRunning ? "Generating profile..." : "Latest profile generation job"}
                  </p>
                  <p className="mt-1 leading-6">{statusMessage(latestBootstrapJob)}</p>
                </StatusBanner>
              ) : null}

              {settingsMessage ? (
                <StatusBanner tone={settingsMessage.tone}>{settingsMessage.text}</StatusBanner>
              ) : null}
            </Card.Content>
            <Card.Footer className="flex flex-wrap items-center justify-end gap-3">
              <Button
                isDisabled={settingsBusy || bootstrapRunning || !interestDescription.trim() || Boolean(currentProfile)}
                onPress={() => void handleSaveAndGenerate()}
              >
                {settingsBusy ? "Saving..." : bootstrapRunning ? "Generating..." : "Save and Generate"}
              </Button>
            </Card.Footer>
          </Card>

          <Card className="border border-(--line) bg-(--paper-accent)">
            <Card.Header className="flex items-start justify-between gap-3">
              <div className="space-y-1">
                <h1 className="text-2xl font-semibold text-(--ink)">Advanced Settings</h1>
              </div>
              <Button
                size="sm"
                variant="outline"
                onPress={() => setAdvancedOpen((current) => !current)}
              >
                {advancedOpen ? "Collapse" : "Expand"}
              </Button>
            </Card.Header>
            {advancedOpen ? (
              <Card.Content className="space-y-1">
                <AdvancedGroup
                  title="AI Overrides"
                  fields={aiAdvancedFields}
                  values={advancedValues}
                  onChange={handleAdvancedValueChange}
                />

                <AdvancedGroup
                  title="Zotero"
                  fields={zoteroFields}
                  values={advancedValues}
                  onChange={handleAdvancedValueChange}
                />

                <AdvancedGroup
                  title="Local App and Files"
                  fields={localAppFields}
                  values={advancedValues}
                  onChange={handleAdvancedValueChange}
                />
              </Card.Content>
            ) : null}
          </Card>
        </div>

        <ProposalDraftCard
          accepting={accepting}
          draft={profileDraft}
          message={proposalMessage}
          onAccept={() => void handleAcceptDraft()}
          onChangeDraft={setProfileDraft}
          onReject={() => void handleRejectProposal()}
          pendingProposal={pendingProposal}
          rejecting={rejecting}
        />
      </div>
    </main>
  );
}
