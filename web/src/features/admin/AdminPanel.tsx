import {Button} from "@heroui/react";
import React from "react";

import {type ReclassifyScope} from "../../api/client";
import type {
  AppMeta,
  AppUpdate,
  ClassifierModelsResponse,
  ClassificationProfile,
  FeedSubscription,
  FeedbackRecord,
  JobInfo,
  ProfileProposal,
  SchedulerSettings,
  SettingsConfigField,
  ClassifierModelsUpdate,
  SettingsConfigUpdate,
} from "../../shared/types";
import {AppTab} from "./AppTab";
import {AdminDisclosure} from "./AdminDisclosure";
import {DashboardTab} from "./DashboardTab";
import {DeepSeekPricingEditor, type DeepSeekPricingEditorHandle} from "./DeepSeekPricingEditor";
import {ClassifierModelsEditor, classifierModelsDraftHasRequiredKeys, classifierModelsUpdateFromDraft, createClassifierModelsDraft, type ClassifierModelsDraft} from "./ClassifierModelsEditor";
import {FeedsTab} from "./FeedsTab";
import {ProfileTab} from "./ProfileTab";
import {SettingsConfigEditor, type SettingsConfigEditorHandle} from "./SettingsConfigEditor";

export type AdminTab = "dashboard" | "feeds" | "profile" | "model" | "app";

const adminTabs: Array<{id: AdminTab; label: string}> = [
  {id: "dashboard", label: "Dashboard"},
  {id: "feeds", label: "Feeds"},
  {id: "profile", label: "Profile"},
  {id: "model", label: "Model"},
  {id: "app", label: "App"},
];

const modelSections = new Set(["Classifier tuning", "Profile model"]);
const pricingSections = new Set(["DeepSeek pricing", "GLM pricing", "Qwen pricing", "MiMo pricing"]);
const appSections = new Set(["Zotero", "Local app"]);

export type AdminPanelProps = {
  activeTab: AdminTab;
  appMeta: AppMeta | null;
  appUpdate: AppUpdate | null;
  appUpdateChecking: boolean;
  configFields: SettingsConfigField[];
  configSaving: boolean;
  classifierModels: ClassifierModelsResponse;
  feedback: FeedbackRecord[];
  feeds: FeedSubscription[];
  feedsSaving: boolean;
  hasFeeds: boolean;
  jobs: JobInfo[];
  onApplyProposal: (id: number, selection?: {accepted_change_ids: string[]; rejected_change_ids: string[]}) => Promise<void> | void;
  onCheckForUpdates: () => void;
  onClose: () => void;
  onDeleteFeedback: (id: number) => void;
  onDeleteScheduler: () => Promise<void>;
  onGenerateProposal: () => void;
  onOpenVerificationInBrowser: (job: JobInfo) => void;
  onReclassify: (scope: ReclassifyScope, limit?: number) => Promise<void> | void;
  onRejectProposal: (id: number) => void;
  onRunSync: (feedURLs?: string[]) => void;
  onStopJob: (jobID: string, jobType: "sync" | "reclassify") => Promise<void> | void;
  onSaveConfig: (fields: Record<string, SettingsConfigUpdate>, classifierModels?: ClassifierModelsUpdate) => Promise<void>;
  onTestClassifierModel: (modelID: string, apiKey?: string) => Promise<JobInfo>;
  onSaveFeeds: (feeds?: FeedSubscription[]) => Promise<boolean | void> | boolean | void;
  onSaveProfile: (profile: ClassificationProfile) => Promise<void> | void;
  onSaveScheduler: (dailyTime: string) => Promise<void>;
  onStartVerification: (job: JobInfo) => void;
  onSubmitVerificationXML: (job: JobInfo, xml: string) => Promise<void> | void;
  onTabChange: (tab: AdminTab) => void;
  open: boolean;
  profile: ClassificationProfile | null;
  profileSaving: boolean;
  proposalGenerating: boolean;
  proposals: ProfileProposal[];
  scheduler: SchedulerSettings | null;
  schedulerSaving: boolean;
  verificationSubmitting: boolean;
  verificationSubmitError: string | null;
};

// 设置抽屉只负责页面导航，各页面自行维护局部交互状态。
export function AdminPanel(props: AdminPanelProps) {
  const modelFields = React.useMemo(
    () => props.configFields.filter((field) => modelSections.has(field.section)),
    [props.configFields],
  );
  const appFields = React.useMemo(
    () => props.configFields.filter((field) => appSections.has(field.section)),
    [props.configFields],
  );
  const pricingFields = React.useMemo(
    () => props.configFields.filter((field) => pricingSections.has(field.section)),
    [props.configFields],
  );

  const [classifierDraft, setClassifierDraft] = React.useState<ClassifierModelsDraft>(() => createClassifierModelsDraft(props.classifierModels));
  const profileModelRef = React.useRef<SettingsConfigEditorHandle | null>(null);
  const advancedModelRef = React.useRef<SettingsConfigEditorHandle | null>(null);
  const pricingRef = React.useRef<DeepSeekPricingEditorHandle | null>(null);
  const dialogRef = React.useRef<HTMLElement | null>(null);
  const closeButtonRef = React.useRef<HTMLButtonElement | null>(null);
  const restoreFocusRef = React.useRef<HTMLElement | null>(null);
  const onCloseRef = React.useRef(props.onClose);
  onCloseRef.current = props.onClose;
  const primaryModelFields = React.useMemo(
    () => modelFields.filter((field) => field.key === "SCIRSS_PROFILE_API_KEY"),
    [modelFields],
  );
  const advancedModelFields = React.useMemo(
    () => modelFields.filter((field) => field.key !== "SCIRSS_PROFILE_API_KEY"),
    [modelFields],
  );
  React.useEffect(() => {
    setClassifierDraft(createClassifierModelsDraft(props.classifierModels));
  }, [props.classifierModels]);

  React.useEffect(() => {
    if (!props.open) return;
    restoreFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const originalBodyOverflow = document.body.style.overflow;
    const originalDocumentOverflow = document.documentElement.style.overflow;
    document.body.style.overflow = "hidden";
    document.documentElement.style.overflow = "hidden";
    window.requestAnimationFrame(() => closeButtonRef.current?.focus());
    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onCloseRef.current();
      if (event.key !== "Tab" || !dialogRef.current) return;
      const focusable = Array.from(dialogRef.current.querySelectorAll<HTMLElement>("button:not([disabled]), a[href], input:not([disabled]), select:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex='-1'])"))
        .filter((element) => element.getClientRects().length > 0);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault();
        first.focus();
      }
    };
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.body.style.overflow = originalBodyOverflow;
      document.documentElement.style.overflow = originalDocumentOverflow;
      document.removeEventListener("keydown", handleKeyDown);
      restoreFocusRef.current?.focus();
    };
  }, [props.open]);

  const saveModelSettings = () => {
    const fields = {
      ...(profileModelRef.current?.getPayload() ?? {}),
      ...(advancedModelRef.current?.getPayload() ?? {}),
      ...(pricingRef.current?.getPayload() ?? {}),
    };
    return props.onSaveConfig(fields, classifierModelsUpdateFromDraft(classifierDraft));
  };

  return (
    <div
      className={props.open ? "fixed inset-0 z-40 flex justify-end overscroll-contain bg-slate-900/20" : "hidden"}
      onMouseDown={(event) => { if (event.target === event.currentTarget) props.onClose(); }}
    >
      <aside ref={dialogRef} aria-labelledby="settings-title" aria-modal="true" className="h-full w-full max-w-[min(1040px,96vw)] overflow-hidden overscroll-contain border-l border-(--line) bg-(--paper-accent) shadow-xl" role="dialog">
        <header className="flex h-18 items-center justify-between gap-4 border-b border-(--line) px-5">
          <div>
            <p className="text-xs font-medium text-muted">FeedMeDaily</p>
            <h2 id="settings-title" className="text-xl font-semibold text-(--ink)">Settings</h2>
          </div>
          <Button ref={closeButtonRef} size="sm" variant="ghost" onPress={props.onClose}>Close</Button>
        </header>

        <div className="grid h-[calc(100%-4.5rem)] min-h-0 grid-rows-[auto_minmax(0,1fr)] md:grid-cols-[176px_minmax(0,1fr)] md:grid-rows-1">
          <nav aria-label="Settings sections" className="overflow-x-auto border-b border-(--line) bg-(--paper) px-3 py-2 md:overflow-y-auto md:border-r md:border-b-0 md:px-3 md:py-4">
            <div className="flex min-w-max gap-1 md:min-w-0 md:flex-col">
              {adminTabs.map((tab) => (
                <Button
                  key={tab.id}
                  aria-current={props.activeTab === tab.id ? "page" : undefined}
                  className="justify-start"
                  size="sm"
                  variant={props.activeTab === tab.id ? "secondary" : "ghost"}
                  onPress={() => props.onTabChange(tab.id)}
                >
                  {tab.label}
                </Button>
              ))}
            </div>
          </nav>

          <main className="min-h-0 overflow-y-auto overscroll-contain bg-(--paper-accent) px-5 py-5 md:px-6">
            <div hidden={props.activeTab !== "dashboard"}>
            <DashboardTab
              feeds={props.feeds}
              hasFeeds={props.hasFeeds}
              jobs={props.jobs}
              onOpenVerificationInBrowser={props.onOpenVerificationInBrowser}
              onReclassify={props.onReclassify}
              onRunSync={props.onRunSync}
              onStopJob={props.onStopJob}
              onStartVerification={props.onStartVerification}
              onSubmitVerificationXML={props.onSubmitVerificationXML}
              verificationSubmitting={props.verificationSubmitting}
              verificationSubmitError={props.verificationSubmitError}
            />
            </div>
            <div hidden={props.activeTab !== "feeds"}>
              <FeedsTab feeds={props.feeds} feedsSaving={props.feedsSaving} onSaveFeeds={props.onSaveFeeds} />
            </div>
            <div hidden={props.activeTab !== "profile"}>
            <ProfileTab feedback={props.feedback} onApplyProposal={props.onApplyProposal} onDeleteFeedback={props.onDeleteFeedback} onGenerateProposal={props.onGenerateProposal} onRejectProposal={props.onRejectProposal} onSaveProfile={props.onSaveProfile} profile={props.profile} profileSaving={props.profileSaving} proposalGenerating={props.proposalGenerating} proposals={props.proposals} />
            </div>
            <div className="space-y-6" hidden={props.activeTab !== "model"}>
              <div className="flex flex-wrap items-start justify-between gap-3 border-b border-(--line) pb-4">
                <div>
                  <h2 className="text-xl font-semibold text-(--ink)">Model</h2>
                  <p className="mt-1 text-sm text-muted">Choose the models used for classification and profile generation.</p>
                </div>
                  <Button
                    isDisabled={props.configSaving || classifierDraft.enabledModelIds.length === 0 || !classifierDraft.enabledModelIds.includes(classifierDraft.defaultModelId) || !classifierModelsDraftHasRequiredKeys(classifierDraft, props.classifierModels)}
                    size="sm"
                    onPress={() => void saveModelSettings()}
                  >
                    {props.configSaving ? "Saving..." : "Save model settings"}
                  </Button>
              </div>
              <section className="space-y-4 border-b border-(--line) pb-6">
                <h3 className="text-sm font-semibold text-(--ink)">Classifier</h3>
                  <ClassifierModelsEditor
                    draft={classifierDraft}
                    jobs={props.jobs}
                    models={props.classifierModels}
                    onChange={setClassifierDraft}
                    onTest={props.onTestClassifierModel}
                  />
              </section>
              <section className="border-b border-(--line) pb-6">
                <h3 className="mb-3 text-sm font-semibold text-(--ink)">Profile generator</h3>
                <SettingsConfigEditor ref={profileModelRef} fields={primaryModelFields} hideGroupTitles saving={props.configSaving} showHeader={false} showSaveAction={false} title="Profile generator" onSave={props.onSaveConfig} />
              </section>
              <AdminDisclosure title="Advanced model settings">
                <div>
                  <SettingsConfigEditor ref={advancedModelRef} fields={advancedModelFields} saving={props.configSaving} showHeader={false} showSaveAction={false} title="Advanced model settings" onSave={props.onSaveConfig} />
                  <DeepSeekPricingEditor ref={pricingRef} fields={pricingFields} saving={props.configSaving} showSaveAction={false} onSave={props.onSaveConfig} />
                </div>
              </AdminDisclosure>
            </div>
            <div hidden={props.activeTab !== "app"}>
              <AppTab appMeta={props.appMeta} appUpdate={props.appUpdate} appUpdateChecking={props.appUpdateChecking} configFields={appFields} configSaving={props.configSaving} onCheckForUpdates={props.onCheckForUpdates} onDeleteScheduler={props.onDeleteScheduler} onSaveConfig={props.onSaveConfig} onSaveScheduler={props.onSaveScheduler} scheduler={props.scheduler} schedulerSaving={props.schedulerSaving} />
            </div>
          </main>
        </div>
      </aside>
    </div>
  );
}
