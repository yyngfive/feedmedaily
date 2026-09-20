// Package journals resolves display-only journal identities without changing
// bibliographic metadata or making network requests.
package journals

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

//go:embed catalog.json
var catalogJSON []byte

type Identity struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

const (
	feedScopeSingleJournal      = "single_journal"
	feedScopeMultiJournal       = "multi_journal"
	feedScopeSubjectCollection  = "subject_collection"
	feedScopePlatformCollection = "platform_collection"
)

type feedMapping struct {
	Label string
	Scope string
}

var byURL, knownNames, preprintSubjects = loadCatalog()
var genericSuffixFallbacks = []*regexp.Regexp{
	regexp.MustCompile(`(?i)^(.+?)\s+advanceAccess$`),
	regexp.MustCompile(`(?i)^(.+),\s*Volume\s+\d+,\s*Issue\s+\d+(?:\s*[-–]\s*\d+)?(?:,\s*pp\.?\s*.+)?$`),
	regexp.MustCompile(`(?i)^(.+),\s*Vol(?:ume)?\.?\s+\d+\b.*$`),
}
var preprintSubjectCollection = regexp.MustCompile(`(?i)^(biorxiv|medrxiv)\s+subject\s+collection\s*:\s*(.+)$`)
var preprintPlatforms = map[string]string{"biorxiv": "bioRxiv", "medrxiv": "medRxiv"}

func normalized(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

func loadCatalog() (map[string]feedMapping, map[string]string, map[string]string) {
	var catalog struct {
		Feeds []struct {
			URL              string `json:"url"`
			Journal          string `json:"journal"`
			CanonicalJournal string `json:"canonical_journal"`
			SourceJournal    string `json:"source_journal"`
			Publisher        string `json:"publisher"`
			FeedScope        string `json:"feed_scope"`
		}
		SubjectCollections []string `json:"subject_collections"`
	}
	if err := json.Unmarshal(catalogJSON, &catalog); err != nil {
		panic(err)
	}
	urls, names, subjects := map[string]feedMapping{}, map[string]string{}, map[string]string{}
	for _, f := range catalog.Feeds {
		label := strings.Join(strings.Fields(f.Journal), " ")
		if label == "" {
			label = strings.Join(strings.Fields(f.SourceJournal), " ")
		}
		if label == "" {
			label = strings.Join(strings.Fields(f.CanonicalJournal), " ")
		}
		if label == "" {
			continue
		}
		if strings.TrimSpace(f.URL) != "" {
			urls[strings.TrimSpace(f.URL)] = feedMapping{Label: label, Scope: strings.ToLower(strings.TrimSpace(f.FeedScope))}
		}
		names[normalized(label)] = label
		if canonical := strings.Join(strings.Fields(f.CanonicalJournal), " "); canonical != "" {
			names[normalized(canonical)] = canonical
		}
		if sourceLabel := strings.Join(strings.Fields(f.SourceJournal), " "); sourceLabel != "" {
			names[normalized(sourceLabel)] = label
			addLatestFeedAliases(names, f.Publisher, sourceLabel, label)
		}
	}
	for _, name := range catalog.SubjectCollections {
		label := strings.Join(strings.Fields(name), " ")
		if label != "" {
			subjects[normalized(label)] = label
		}
	}
	return urls, names, subjects
}

func addLatestFeedAliases(names map[string]string, publisher, sourceLabel, canonical string) {
	parts := strings.SplitN(sourceLabel, ":", 2)
	if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), strings.TrimSpace(publisher)) {
		return
	}
	suffix := normalized(parts[1])
	if suffix != "latest" && suffix != "latest preprints" {
		return
	}
	names[normalized(strings.TrimSpace(publisher)+": Latest Preprints")] = canonical
}

func cleanName(value string) string {
	name := strings.Join(strings.Fields(value), " ")
	// Exact aliases generated from sci-rss-list always win. The suffix rules below
	// are generic fallbacks and only apply when their base name is in that catalog.
	if canonical, ok := knownNames[normalized(name)]; ok {
		return canonical
	}
	if match := preprintSubjectCollection.FindStringSubmatch(name); match != nil {
		if platform := preprintPlatforms[normalized(match[1])]; platform != "" {
			name = platform + ": " + strings.TrimSpace(match[2])
			if canonical, ok := knownNames[normalized(name)]; ok {
				return canonical
			}
			if canonical, ok := preprintSubjects[normalized(name)]; ok {
				return canonical
			}
		}
	}
	for _, suffix := range genericSuffixFallbacks {
		if match := suffix.FindStringSubmatch(name); match != nil && len(match) > 1 {
			if canonical, ok := knownNames[normalized(match[1])]; ok {
				return canonical
			}
		}
	}
	if canonical, ok := preprintSubjects[normalized(name)]; ok {
		return canonical
	}
	return name
}

func Resolve(sourceURL string, journal, feedTitle *string) Identity {
	journalName := ""
	if journal != nil {
		journalName = strings.TrimSpace(*journal)
	}
	feedName := ""
	if feedTitle != nil {
		feedName = strings.TrimSpace(*feedTitle)
	}

	source, hasSource := byURL[strings.TrimSpace(sourceURL)]
	if hasSource {
		switch source.Scope {
		case feedScopeSingleJournal, feedScopeSubjectCollection, feedScopePlatformCollection:
			if source.Label != "" {
				return identityFor(source.Label)
			}
		case feedScopeMultiJournal:
			if name := cleanName(journalName); name != "" {
				return identityFor(name)
			}
		}
	}

	name := ""
	if journalName != "" && feedName != "" && normalized(journalName) != normalized(feedName) {
		name = cleanName(journalName)
	}
	if name == "" {
		name = source.Label
	}
	if name == "" && journalName != "" {
		name = cleanName(journalName)
	}
	if name == "" && feedName != "" {
		name = cleanName(feedName)
	}
	if name == "" {
		return Identity{Key: "unknown", Label: "Unknown journal"}
	}
	return identityFor(name)
}

func identityFor(name string) Identity {
	sum := sha256.Sum256([]byte(normalized(name)))
	return Identity{Key: fmt.Sprintf("journal:%x", sum), Label: name}
}
