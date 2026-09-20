package journals

import "testing"

func TestResolve(t *testing.T) {
	for _, tc := range []struct{ url, name, feed, want string }{
		{"https://pubs.acs.org/rss/jacsat/asap.xml", "Journal of the American Chemical Society advanceAccess", "Journal of the American Chemical Society advanceAccess", "Journal of the American Chemical Society"},
		{"https://pubs.acs.org/rss/jacsat/currentIssue.xml", "Journal of the American Chemical Society Current Issue", "Journal of the American Chemical Society Current Issue", "Journal of the American Chemical Society"},
		{"http://feeds.rsc.org/rss/cc", "RSC - Chem. Commun. latest articles", "", "Chemical Communications"},
		{"http://feeds.rsc.org/rss/sc", "RSC - Chem. Sci. latest articles", "", "Chemical Science"},
		{"", "ACS Nano advanceAccess", "", "ACS Nano"},
		{"", "Analytical Chemistry (ASAP)", "", "Analytical Chemistry"},
		{"", "Cell, Volume 189, Issue 10–11", "", "Cell"},
		{"", "ChemRxiv: Latest Preprints", "", "ChemRxiv"},
		{"", "ChemRxiv: Latest", "", "ChemRxiv"},
		{"", "Chemrxiv", "", "ChemRxiv"},
		{"", "Physical Review Letters (Recently Published)", "", "Physical Review Letters"},
		{"", "Physical Review A (Editors' Suggestions)", "", "Physical Review A"},
		{"", "Physics (Recently Published)", "", "Physics"},
		{"", "APS Journals (All Editors' Suggestions)", "", "APS Journals (All Editors' Suggestions)"},
		{"", "Optica, Vol. 13, Issue 10, pp. 1943-1950", "", "Optica"},
		{"", "bioRxiv Subject Collection: Molecular Biology", "", "bioRxiv: Molecular Biology"},
		{"", "bioRxiv Subject Collection: Synthetic biology", "", "bioRxiv: Synthetic Biology"},
		{"", "bioRxiv Subject Collection: Systems Biology", "", "bioRxiv: Systems Biology"},
		{"", "medRxiv Subject Collection: Infectious diseases", "", "medRxiv: Infectious Diseases"},
		{"", "bioRxiv Subject Collection: Newly Named Field", "", "bioRxiv: Newly Named Field"},
		{"https://example.org/aggregate", "Nature", "All science", "Nature"},
		{"", "Unknown (2026): Part 2", "", "Unknown (2026): Part 2"},
		{"", "Unlisted Journal advanceAccess", "", "Unlisted Journal advanceAccess"},
		{"", "Unlisted Journal, Volume 189, Issue 10", "", "Unlisted Journal, Volume 189, Issue 10"},
		{"", "Journal (New York, N.Y.)", "", "Journal (New York, N.Y.)"},
		{"", "Nature advanceAccess", "", "Nature"},
		{"", "", "Custom RSS", "Custom RSS"},
		{"", "", "", "Unknown journal"},
	} {
		t.Run(tc.name+tc.url, func(t *testing.T) {
			got := Resolve(tc.url, &tc.name, &tc.feed)
			if got.Label != tc.want {
				t.Fatalf("got %q want %q", got.Label, tc.want)
			}
			if got.Key == "" {
				t.Fatal("empty identity")
			}
		})
	}
	a, b := "Custom   JOURNAL", "custom journal"
	if Resolve("", &a, nil).Key != Resolve("", &b, nil).Key {
		t.Fatal("case and whitespace must share an identity")
	}
	molecular := "bioRxiv Subject Collection: Molecular Biology"
	canonicalMolecular := "bioRxiv: Molecular Biology"
	synthetic := "bioRxiv Subject Collection: Synthetic biology"
	medical := "medRxiv Subject Collection: Infectious diseases"
	if Resolve("", &molecular, nil).Key != Resolve("", &canonicalMolecular, nil).Key {
		t.Fatal("a known subject collection alias must share the canonical identity")
	}
	if Resolve("", &molecular, nil).Key == Resolve("", &synthetic, nil).Key {
		t.Fatal("different bioRxiv subject collections must stay distinct")
	}
	if Resolve("", &molecular, nil).Key == Resolve("", &medical, nil).Key {
		t.Fatal("bioRxiv and medRxiv subject collections must stay distinct")
	}
	if Resolve("", nil, nil).Key != "unknown" {
		t.Fatal("missing metadata")
	}
}

func TestArticleJournalPrecedesFeedLabel(t *testing.T) {
	var url string
	for candidate, mapping := range byURL {
		if mapping.Label == "APS Journals (All Editors' Suggestions)" && mapping.Scope == feedScopeMultiJournal {
			url = candidate
			break
		}
	}
	if url == "" {
		t.Fatal("source-derived APS cross-journal feed mapping is missing")
	}
	articleJournal := "Physical Review Letters"
	feedTitle := "APS Journals (All Editors' Suggestions)"
	got := Resolve(url, &articleJournal, &feedTitle)
	if got.Label != articleJournal {
		t.Fatalf("article journal should override a cross-journal feed label: got %q want %q", got.Label, articleJournal)
	}
}

func TestSingleJournalSourceURLPrecedesArticleCitation(t *testing.T) {
	const cellURL = "https://www.cell.com/cell/current.rss"
	mapping, ok := byURL[cellURL]
	if !ok || mapping.Scope != feedScopeSingleJournal || mapping.Label != "Cell" {
		t.Fatalf("Cell source must be cataloged as a single-journal feed: got %#v", mapping)
	}

	// Removing the name alias proves that the result comes from the URL catalog,
	// rather than the generic volume/issue fallback.
	cellKey := normalized("Cell")
	cellName, hadCellName := knownNames[cellKey]
	delete(knownNames, cellKey)
	defer func() {
		if hadCellName {
			knownNames[cellKey] = cellName
		}
	}()

	articleJournal := "Cell, Volume 189, Issue 10–11"
	feedTitle := "Cell"
	got := Resolve(cellURL, &articleJournal, &feedTitle)
	if got.Label != "Cell" {
		t.Fatalf("single-journal URL mapping should win over article citation: got %q", got.Label)
	}
}

func TestSubjectCollectionUsesFeedIdentity(t *testing.T) {
	const subjectURL = "https://connect.medrxiv.org/medrxiv_xml.php?subject=health_economics"
	mapping, ok := byURL[subjectURL]
	if !ok || mapping.Scope != feedScopeSubjectCollection {
		t.Fatalf("medRxiv category feed must be marked as a subject collection: got %#v", mapping)
	}
	articleJournal := "Nature"
	feedTitle := "medRxiv: Health Economics"
	got := Resolve(subjectURL, &articleJournal, &feedTitle)
	if got.Label != "medRxiv: Health Economics" {
		t.Fatalf("subject collection URL mapping should preserve its category identity: got %q", got.Label)
	}
}

func TestSourceFeedLabelsResolveFromCatalog(t *testing.T) {
	var apsURL string
	for candidate, mapping := range byURL {
		if mapping.Label == "Physical Review Letters" {
			apsURL = candidate
			break
		}
	}
	if apsURL == "" {
		t.Fatal("APS journal identity was not generated from the source catalog")
	}
	feedLabel := "Physical Review Letters (Recently Published)"
	if got := cleanName(feedLabel); got != "Physical Review Letters" {
		t.Fatalf("exact source-list alias must resolve before generic fallback: got %q", got)
	}
	got := Resolve(apsURL, &feedLabel, &feedLabel)
	if got.Label != "Physical Review Letters" {
		t.Fatalf("feed-type suffix must not enter display name: got %q", got.Label)
	}
}
