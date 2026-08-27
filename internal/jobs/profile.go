package jobs

import (
	"fmt"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/llmusage"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func GenerateInitialProfileProposal(settings config.Settings, interestDescription string, name *string, progress ProgressFunc, collectors ...*llmusage.Collector) (map[string]any, error) {
	// 用 Go 原生 profile generation 生成首个 proposal，并确保 SQLite 可写入。
	current, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return nil, err
	}
	if current != nil {
		return nil, fmt.Errorf("A classification profile already exists.")
	}
	EmitProgress(progress, StepProgress(
		"profile.bootstrap.preparing",
		"profile-bootstrap",
		1,
		4,
		FormatStepMessage(1, 4, "Preparing initial profile request."),
	))
	EmitProgress(progress, StepProgress(
		"profile.bootstrap.generating",
		"profile-bootstrap",
		2,
		4,
		FormatStepMessage(2, 4, "Generating initial profile proposal."),
	))
	draft, err := profile.GenerateInitialProfileProposal(settings, interestDescription, name, firstJobUsageCollector(collectors))
	if err != nil {
		return nil, err
	}
	EmitProgress(progress, StepProgress(
		"profile.bootstrap.validating",
		"profile-bootstrap",
		3,
		4,
		FormatStepMessage(3, 4, "Validating generated profile."),
	))
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	defer sqliteStore.Close()
	EmitProgress(progress, StepProgress(
		"profile.bootstrap.saving",
		"profile-bootstrap",
		4,
		4,
		FormatStepMessage(4, 4, "Saving initial profile proposal."),
	))
	item, err := sqliteStore.SaveProfileProposal(
		draft.Summary,
		draft.BaseProfileVersion,
		draft.ProposedProfile,
		draft.Changes,
		draft.RuleDelta,
		draft.SourceFeedbackIDs,
		draft.Model,
		time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"proposal_id": item.ID}, nil
}

func GenerateProfileProposal(settings config.Settings, progress ProgressFunc, collectors ...*llmusage.Collector) (map[string]any, error) {
	// 从当前 profile 和 open feedback 生成一个新的 proposal 并落库。
	current, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("No classification profile exists yet.")
	}
	EmitProgress(progress, StepProgress(
		"profile.proposal.collecting_feedback",
		"profile-proposal",
		1,
		4,
		FormatStepMessage(1, 4, "Collecting feedback and current profile context."),
	))
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	defer sqliteStore.Close()
	contexts, err := sqliteStore.ListOpenFeedbackContexts()
	if err != nil {
		return nil, err
	}
	feedbackItems := make([]profile.FeedbackProposalContext, 0, len(contexts))
	for _, item := range contexts {
		feedbackItems = append(feedbackItems, profile.FeedbackProposalContext{
			FeedbackID:         item.FeedbackID,
			PaperID:            item.PaperID,
			PaperTitle:         item.PaperTitle,
			Journal:            item.Journal,
			Abstract:           item.Abstract,
			OriginalRelevance:  item.OriginalRelevance,
			CorrectedRelevance: item.CorrectedRelevance,
			Note:               item.Note,
		})
	}
	EmitProgress(progress, StepProgress(
		"profile.proposal.generating",
		"profile-proposal",
		2,
		4,
		FormatStepMessage(2, 4, "Generating profile proposal."),
	))
	draft, err := profile.GenerateProfileProposal(settings, current, feedbackItems, firstJobUsageCollector(collectors))
	if err != nil {
		return nil, err
	}
	if draft.Rejection != nil {
		EmitProgress(progress, StepProgress(
			"profile.proposal.rejected",
			"profile-proposal",
			3,
			4,
			"Profile proposal rejected by safety review.",
		))
		return map[string]any{
			"accepted":        false,
			"hard_rejected":   draft.Rejection.HardRejected,
			"summary":         draft.Rejection.Summary,
			"blocking_issues": draft.Rejection.BlockingIssues,
			"required_fixes":  draft.Rejection.RequiredFixes,
		}, nil
	}
	EmitProgress(progress, StepProgress(
		"profile.proposal.validating",
		"profile-proposal",
		3,
		4,
		FormatStepMessage(3, 4, "Validating generated profile proposal."),
	))
	EmitProgress(progress, StepProgress(
		"profile.proposal.saving",
		"profile-proposal",
		4,
		4,
		FormatStepMessage(4, 4, "Saving profile proposal."),
	))
	item, err := sqliteStore.SaveProfileProposal(
		draft.Summary,
		draft.BaseProfileVersion,
		draft.ProposedProfile,
		draft.Changes,
		draft.RuleDelta,
		draft.SourceFeedbackIDs,
		draft.Model,
		time.Now().UTC(),
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{"accepted": true, "proposal_id": item.ID}, nil
}

func firstJobUsageCollector(items []*llmusage.Collector) *llmusage.Collector {
	if len(items) == 0 {
		return nil
	}
	return items[0]
}
