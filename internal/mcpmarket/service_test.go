package mcpmarket

import "testing"

func TestPublishAndSearch(t *testing.T) {
	svc := New()
	svc.SeedDefaultListings()

	// Search all
	results := svc.SearchListings("", "")
	if len(results) < 3 {
		t.Fatalf("expected 3+ listings, got %d", len(results))
	}

	// Search by category
	dbResults := svc.SearchListings("", "database")
	if len(dbResults) != 1 {
		t.Fatalf("expected 1 database, got %d", len(dbResults))
	}

	// Search by Korean text
	krResults := svc.SearchListings("한국어", "")
	if len(krResults) != 1 {
		t.Fatalf("expected 1 Korean result, got %d", len(krResults))
	}
}

func TestAddVersionAndReview(t *testing.T) {
	svc := New()
	listing, _ := svc.PublishListing(MCPListing{
		Name: "Test MCP", NameKo: "테스트 MCP",
		Publisher: "test", Category: "tools",
	})

	// Add version
	err := svc.AddVersion(listing.ID, MCPVersion{
		Version:     "1.0.0",
		PackageHash: "sha256:abc",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, _ := svc.GetListing(listing.ID)
	if got.LatestVersion != "1.0.0" {
		t.Fatal("expected version 1.0.0")
	}

	// Add review
	svc.AddReview(listing.ID, MCPReview{
		UserID: "u1", UserName: "김개발", Rating: 5, Comment: "좋아요!",
	})
	got, _ = svc.GetListing(listing.ID)
	if got.Rating != 5.0 {
		t.Fatalf("expected rating 5.0, got %f", got.Rating)
	}
}

func TestDeprecate(t *testing.T) {
	svc := New()
	listing, _ := svc.PublishListing(MCPListing{Name: "Test"})
	svc.DeprecateListing(listing.ID, "deprecated")

	got, _ := svc.GetListing(listing.ID)
	if got.Status != "deprecated" {
		t.Fatal("expected deprecated")
	}

	// Should not appear in search
	results := svc.SearchListings("", "")
	for _, r := range results {
		if r.ID == listing.ID {
			t.Fatal("deprecated listing should not appear in search")
		}
	}
}

func TestCategories(t *testing.T) {
	cats := Categories()
	if len(cats) < 6 {
		t.Fatalf("expected 6+ categories, got %d", len(cats))
	}
}
