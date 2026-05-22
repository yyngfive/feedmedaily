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
