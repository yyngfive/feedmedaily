package jobs

import (
	"fmt"
	"time"

	"github.com/yyngfive/scirssagent/internal/config"
	"github.com/yyngfive/scirssagent/internal/profile"
	store "github.com/yyngfive/scirssagent/internal/store/sqlite"
)

func GenerateInitialProfileProposal(settings config.Settings, interestDescription string, name *string, progress ProgressFunc) (map[string]any, error) {
	// 用 Go 原生 profile generation 生成首个 proposal，并确保 SQLite 可写入。
	current, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return nil, err
	}
	if current != nil {
		return nil, fmt.Errorf("A classification profile already exists.")
	}
	if progress != nil {
		progress("profile.bootstrap.generating", "Generating the initial classification profile proposal.")
	}
	draft, err := profile.GenerateInitialProfileProposal(settings, interestDescription, name)
	if err != nil {
		return nil, err
	}
	sqliteStore, err := store.OpenOrCreate(settings.DatabasePath)
	if err != nil {
		return nil, err
	}
	defer sqliteStore.Close()
	item, err := sqliteStore.SaveProfileProposal(
		draft.Summary,
		draft.ProposedProfile,
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

func GenerateProfileProposal(settings config.Settings, progress ProgressFunc) (map[string]any, error) {
	// 从当前 profile 和 open feedback 生成一个新的 proposal 并落库。
	current, err := profile.ReadCurrent(settings.ProfilePath)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, fmt.Errorf("No classification profile exists yet.")
	}
	if progress != nil {
		progress("profile.proposal.collecting_feedback", "Collecting feedback for profile review.")
	}
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
	if progress != nil {
		progress("profile.proposal.generating", "Generating profile proposal.")
	}
	draft, err := profile.GenerateProfileProposal(settings, current, feedbackItems)
	if err != nil {
		return nil, err
	}
	item, err := sqliteStore.SaveProfileProposal(
		draft.Summary,
		draft.ProposedProfile,
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
