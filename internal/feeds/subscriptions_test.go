package feeds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSubscriptionsMissingFileReturnsEmptyList(t *testing.T) {
	feeds, err := ReadSubscriptions(filepath.Join(t.TempDir(), "rss_feeds.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 0 {
		t.Fatalf("feeds = %#v", feeds)
	}
}

func TestWriteSubscriptionsNormalizesAndDeduplicatesByURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "rss_feeds.json")
	feeds, err := WriteSubscriptions(path, []Subscription{
		{Journal: " Nature ", URL: "https://www.nature.com/nature.rss"},
		{Journal: "Nature Duplicate", URL: "https://www.nature.com/nature.rss"},
		{Journal: "Science", URL: "https://www.science.org/rss/news_current.xml"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 {
		t.Fatalf("feeds = %#v", feeds)
	}
	if feeds[0].Journal != "Nature" {
		t.Fatalf("journal was not normalized: %#v", feeds[0])
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}

	roundTrip, err := ReadSubscriptions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTrip) != 2 || roundTrip[1].Journal != "Science" {
		t.Fatalf("round trip = %#v", roundTrip)
	}
}

func TestWriteSubscriptionsRejectsInvalidInputs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rss_feeds.json")
	if _, err := WriteSubscriptions(path, []Subscription{{Journal: "", URL: "https://example.com/rss"}}); err == nil {
		t.Fatal("expected blank journal to fail")
	}
	if _, err := WriteSubscriptions(path, []Subscription{{Journal: "Bad", URL: "ftp://example.com/rss"}}); err == nil {
		t.Fatal("expected non-http URL to fail")
	}
}

func TestNormalizeSubscriptionUpgradesLegacyCellURLToHTTPS(t *testing.T) {
	feed, err := NormalizeSubscription(Subscription{
		Journal: "Cell",
		URL:     "http://www.cell.com/cell/current.rss",
	})
	if err != nil {
		t.Fatal(err)
	}
	if feed.URL != "https://www.cell.com/cell/current.rss" {
		t.Fatalf("unexpected url: %#v", feed.URL)
	}
}

func TestWriteSubscriptionsNormalizesPrivateEmailSource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "rss_feeds.json")
	feeds, err := WriteSubscriptions(path, []Subscription{
		{Journal: "RSC journals (email alerts)", EmailSource: " rsc-email-alerts "},
		{Journal: "RSC duplicate", EmailSource: "rsc-email-alerts"},
		{Journal: "Nature", URL: "https://www.nature.com/nature.rss"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(feeds) != 2 {
		t.Fatalf("feeds = %#v", feeds)
	}
	email := feeds[0]
	if email.URL != "" || !email.Private || email.EmailSource != "rsc-email-alerts" {
		t.Fatalf("email subscription = %#v", email)
	}
	if email.SubscriptionIdentity() != "email:rsc-email-alerts" {
		t.Fatalf("identity = %q", email.SubscriptionIdentity())
	}

	roundTrip, err := ReadSubscriptions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(roundTrip) != 2 || roundTrip[0].EmailSource != "rsc-email-alerts" || roundTrip[0].Private != true {
		t.Fatalf("roundTrip = %#v", roundTrip)
	}
}

func TestNormalizeSubscriptionStillRequiresURLOrEmailSource(t *testing.T) {
	if _, err := NormalizeSubscription(Subscription{Journal: "Empty"}); err == nil {
		t.Fatal("expected error for subscription without URL and email source")
	}
	normalized, err := NormalizeSubscription(Subscription{Journal: "Email", URL: "https://example.com/rss", EmailSource: "rsc-email-alerts"})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.URL != "" || !normalized.Private {
		t.Fatalf("email source must drop the stored URL: %#v", normalized)
	}
}
