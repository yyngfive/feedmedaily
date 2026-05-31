package jobs

import "fmt"

type ProgressMode string

const (
	ProgressModeItem ProgressMode = "item"
	ProgressModeStep ProgressMode = "step"
)

type ProgressUpdate struct {
	MessageKey string
	Message    string
	Stage      string
	Current    *int
	Total      *int
	Percent    *int
	Label      string
	Mode       ProgressMode
}

type ProgressFunc func(ProgressUpdate)

func EmitProgress(progress ProgressFunc, update ProgressUpdate) {
	if progress != nil {
		progress(update)
	}
}

func ItemProgress(messageKey string, stage string, current int, total int, label string, message string) ProgressUpdate {
	return ProgressUpdate{
		MessageKey: messageKey,
		Message:    message,
		Stage:      stage,
		Current:    intPtr(current),
		Total:      intPtr(total),
		Label:      label,
		Mode:       ProgressModeItem,
	}
}

func PercentProgress(messageKey string, stage string, current int, total int, message string) ProgressUpdate {
	return ProgressUpdate{
		MessageKey: messageKey,
		Message:    message,
		Stage:      stage,
		Current:    intPtr(current),
		Total:      intPtr(total),
		Percent:    progressPercent(current, total),
		Mode:       ProgressModeItem,
	}
}

func StepProgress(messageKey string, stage string, current int, total int, message string) ProgressUpdate {
	return ProgressUpdate{
		MessageKey: messageKey,
		Message:    message,
		Stage:      stage,
		Current:    intPtr(current),
		Total:      intPtr(total),
		Percent:    progressPercent(current, total),
		Mode:       ProgressModeStep,
	}
}

func progressPercent(current int, total int) *int {
	percent := 0
	if total > 0 && current > 0 {
		percent = current * 100 / total
	}
	return &percent
}

func intPtr(value int) *int {
	next := value
	return &next
}

func FormatStepMessage(current int, total int, detail string) string {
	percent := 0
	if total > 0 && current > 0 {
		percent = current * 100 / total
	}
	return fmt.Sprintf("Step %d/%d (%d%%): %s", current, total, percent, detail)
}

func metadataProgressMessage(current int, total int) string {
	percent := 0
	if total > 0 && current > 0 {
		percent = current * 100 / total
	}
	return fmt.Sprintf("Getting metadata %d/%d (%d%%).", current, total, percent)
}

func classificationProgressMessage(current int, total int) string {
	percent := 0
	if total > 0 && current > 0 {
		percent = current * 100 / total
	}
	return fmt.Sprintf("Classifying papers %d/%d (%d%%).", current, total, percent)
}
