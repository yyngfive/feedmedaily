import {Button, Card} from "@heroui/react";
import React from "react";

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
import {DashboardTab} from "./DashboardTab";
import {DeepSeekPricingEditor} from "./DeepSeekPricingEditor";
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
const pricingSections = new Set(["DeepSeek pricing", "GLM pricing"]);
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
  onAddFeed: () => void;
  onAddFeeds: (feeds: FeedSubscription[]) => void;
  onApplyProposal: (id: number, selection?: {accepted_change_ids: string[]; rejected_change_ids: string[]}) => Promise<void> | void;
  onCheckForUpdates: () => void;
  onClose: () => void;
  onDeleteFeedback: (id: number) => void;
  onDeleteScheduler: () => Promise<void>;
  onFeedChange: (index: number, field: "journal" | "url", value: string) => void;
  onGenerateProposal: () => void;
  onOpenVerificationInBrowser: (job: JobInfo) => void;
  onReclassifyAll: () => void;
  onReclassifyFeedback: () => void;
  onReclassifyRecent: () => void;
  onRejectProposal: (id: number) => void;
  onRemoveFeed: (index: number) => void;
  onRunSync: (feedURLs?: string[]) => void;
  onStopSync: (jobID: string) => Promise<void> | void;
  onSaveConfig: (fields: Record<string, SettingsConfigUpdate>, classifierModels?: ClassifierModelsUpdate) => Promise<void>;
  onTestClassifierModel: (modelID: string, apiKey?: string) => Promise<JobInfo>;
  onSaveFeeds: () => void;
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
  const modelSettingsRef = React.useRef<SettingsConfigEditorHandle | null>(null);
  React.useEffect(() => {
    setClassifierDraft(createClassifierModelsDraft(props.classifierModels));
  }, [props.classifierModels]);

  React.useEffect(() => {
    if (!props.open) return;
    const originalBodyOverflow = document.body.style.overflow;
    const originalDocumentOverflow = document.documentElement.style.overflow;
    document.body.style.overflow = "hidden";
    document.documentElement.style.overflow = "hidden";
    return () => {
      document.body.style.overflow = originalBodyOverflow;
      document.documentElement.style.overflow = originalDocumentOverflow;
    };
  }, [props.open]);

  return (
    <div className={props.open ? "fixed inset-0 z-40 flex justify-end overscroll-contain bg-slate-900/20" : "hidden"}>
      <aside className="h-full w-full max-w-[min(1040px,96vw)] overflow-auto overscroll-contain border-l border-(--line) bg-(--paper) p-5 shadow-xl">
        <div className="w-full">
          <div className="flex items-start justify-between gap-4">
            <div className="min-w-0 space-y-3">
              <h2 className="mt-2 mb-4 text-2xl font-semibold text-(--ink)">Settings</h2>
              <div className="flex flex-wrap gap-2">
                {adminTabs.map((tab) => (
                  <Button key={tab.id} size="sm" variant={props.activeTab === tab.id ? "secondary" : "outline"} onPress={() => props.onTabChange(tab.id)}>
                    {tab.label}
                  </Button>
                ))}
              </div>
            </div>
            <Button size="sm" variant="ghost" onPress={props.onClose}>Close</Button>
          </div>

          <div hidden={props.activeTab !== "dashboard"}>
            <DashboardTab
              appMeta={props.appMeta}
              appUpdate={props.appUpdate}
              appUpdateChecking={props.appUpdateChecking}
              feeds={props.feeds}
              hasFeeds={props.hasFeeds}
              jobs={props.jobs}
              onCheckForUpdates={props.onCheckForUpdates}
              onOpenVerificationInBrowser={props.onOpenVerificationInBrowser}
              onReclassifyAll={props.onReclassifyAll}
              onReclassifyFeedback={props.onReclassifyFeedback}
              onReclassifyRecent={props.onReclassifyRecent}
              onRunSync={props.onRunSync}
              onStopSync={props.onStopSync}
              onStartVerification={props.onStartVerification}
              onSubmitVerificationXML={props.onSubmitVerificationXML}
              verificationSubmitting={props.verificationSubmitting}
              verificationSubmitError={props.verificationSubmitError}
            />
          </div>
          <div hidden={props.activeTab !== "feeds"}>
            <FeedsTab feeds={props.feeds} feedsSaving={props.feedsSaving} onAddFeed={props.onAddFeed} onAddFeeds={props.onAddFeeds} onFeedChange={props.onFeedChange} onRemoveFeed={props.onRemoveFeed} onSaveFeeds={props.onSaveFeeds} />
          </div>
          <div hidden={props.activeTab !== "profile"}>
            <ProfileTab feedback={props.feedback} onApplyProposal={props.onApplyProposal} onDeleteFeedback={props.onDeleteFeedback} onGenerateProposal={props.onGenerateProposal} onRejectProposal={props.onRejectProposal} onSaveProfile={props.onSaveProfile} profile={props.profile} profileSaving={props.profileSaving} proposalGenerating={props.proposalGenerating} proposals={props.proposals} />
          </div>
          <div className="mt-5 space-y-5" hidden={props.activeTab !== "model"}>
            <Card className="rounded-md border border-(--line) bg-(--paper-accent) shadow-none">
              <Card.Header><h3 className="text-xl font-semibold text-(--ink)">Model</h3></Card.Header>
              <Card.Content>
                <div className="mb-5 flex justify-start">
                  <Button
                    isDisabled={props.configSaving || classifierDraft.enabledModelIds.length === 0 || !classifierDraft.enabledModelIds.includes(classifierDraft.defaultModelId) || !classifierModelsDraftHasRequiredKeys(classifierDraft, props.classifierModels)}
                    size="sm"
                    onPress={() => void props.onSaveConfig(modelSettingsRef.current?.getPayload() ?? {}, classifierModelsUpdateFromDraft(classifierDraft))}
                  >
                    {props.configSaving ? "Saving..." : "Save model settings"}
                  </Button>
                </div>
                <div className="space-y-4 border-b border-(--line) pb-5">
                  <ClassifierModelsEditor
                    draft={classifierDraft}
                    jobs={props.jobs}
                    models={props.classifierModels}
                    onChange={setClassifierDraft}
                    onTest={props.onTestClassifierModel}
                  />
                </div>
                <div className="pt-5">
                  <SettingsConfigEditor ref={modelSettingsRef} fields={modelFields} saving={props.configSaving} showSaveAction={false} title="Profile and classifier tuning" onSave={props.onSaveConfig} />
                </div>
                <DeepSeekPricingEditor fields={pricingFields} saving={props.configSaving} onSave={props.onSaveConfig} />
              </Card.Content>
            </Card>
          </div>
          <div hidden={props.activeTab !== "app"}>
            <AppTab configFields={appFields} configSaving={props.configSaving} onDeleteScheduler={props.onDeleteScheduler} onSaveConfig={props.onSaveConfig} onSaveScheduler={props.onSaveScheduler} scheduler={props.scheduler} schedulerSaving={props.schedulerSaving} />
          </div>
        </div>
      </aside>
    </div>
  );
}
