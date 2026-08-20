package leaderboard

import (
	"testing"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func lbDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/lb.db"), &gorm.Config{Logger: logger.Discard})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []interface{}{
		&models.User{}, &models.Organization{}, &models.Session{}, &models.WorkObjective{},
		&models.ActionEnvelope{}, &models.SecurityFinding{}, &models.PromptExchange{},
		&models.ScorecardRubric{}, &models.ScorecardPeriod{}, &models.ScorecardSnapshot{},
		&models.ScorecardCorrection{}, &models.ScorecardReview{},
	} {
		if err := db.AutoMigrate(m); err != nil {
			t.Fatal(err)
		}
	}
	return db
}

func testPeriod() models.ScorecardPeriod {
	now := time.Now().UTC()
	return models.ScorecardPeriod{
		PeriodID: "p1", OrganizationID: "org1", PeriodType: "rolling_90d",
		StartAt: now.AddDate(0, 0, -90), EndAt: now.Add(time.Hour),
		Status: "running", RubricID: "rb1",
	}
}

func seedObjective(db *gorm.DB, id, owner string, t *testing.T, status string) models.WorkObjective {
	t.Helper()
	o := models.WorkObjective{
		OrganizationID: "org1", ObjectiveID: id, WorkType: "feature", SizeBand: "medium",
		OwnerID: owner, Status: status, CoalesceKey: "",
		PassedFirstGate: true, AvoidedRework: true, NoRegression: true,
		ModelTurns: 10, ActiveElapsedMs: 1000000, SourcedBy: "test",
		StartedAt: time.Now().AddDate(0, 0, -2), AcceptedAt: time.Now(),
	}
	if err := db.Create(&o).Error; err != nil {
		t.Fatal(err)
	}
	return o
}

// Weights MUST fall within Patty guardrails: sum to 100, quality >= 20,
// security >= 20, no property > 40.
func TestValidateWeights(t *testing.T) {
	if err := ValidateWeights(30, 25, 30, 15); err != nil {
		t.Fatalf("default weights should be valid: %v", err)
	}
	if err := ValidateWeights(10, 10, 70, 10); err == nil {
		t.Fatalf("security >40 should fail")
	}
	if err := ValidateWeights(40, 10, 40, 10); err == nil {
		t.Fatalf("quality <20 should fail")
	}
	if err := ValidateWeights(30, 30, 40, 1); err == nil {
		t.Fatalf("sum != 100 should fail")
	}
}

// Insufficient evidence (<5 accepted outcomes or <20 governed actions) → no
// score, no rank, 근거 부족 explanation.
func TestInsufficientEvidenceNoRank(t *testing.T) {
	db := lbDB(t)
	svc := New(db)
	period := testPeriod()
	if err := db.Create(&period).Error; err != nil {
		t.Fatal(err)
	}
	// Only 2 accepted outcomes — below min of 5.
	seedObjective(db, "o1", "u1", t, "accepted")
	seedObjective(db, "o2", "u1", t, "accepted")
	scores, err := svc.GeneratePeriodScores("org1", period.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 1 {
		t.Fatalf("expected 1 score row, got %d", len(scores))
	}
	if scores[0].Sufficient {
		t.Fatalf("below-min evidence scored as sufficient: %+v", scores[0])
	}
	if scores[0].Rank != 0 || scores[0].Overall != 0 {
		t.Fatalf("insufficient evidence produced a rank/score: %+v", scores[0])
	}
}

// Split objectives sharing the same coalesce key yield ONE delivery credit
// (anti-gaming: splitting work must not multiply accepted-delivery counts).
func TestCoalesceSplitObjectives(t *testing.T) {
	db := lbDB(t)
	svc := New(db)
	period := testPeriod()
	db.Create(&period)
	// 6 accepted objectives: 3 share a coalesce key (one split) → count 4.
	for i := 1; i <= 4; i++ {
		seedObjective(db, "split"+string(rune('a'+i)), "u2", t, "accepted")
	}
	for i := 1; i <= 3; i++ {
		o := models.WorkObjective{
			OrganizationID: "org1", ObjectiveID: "spl" + string(rune('a'+i)), SizeBand: "small", WorkType: "feature",
			OwnerID: "u2", Status: "accepted", CoalesceKey: "OBJECTIVE-1",
			PassedFirstGate: true, AvoidedRework: true, NoRegression: true, ModelTurns: 3, ActiveElapsedMs: 50000, SourcedBy: "test",
			StartedAt: time.Now().AddDate(0, 0, -2), AcceptedAt: time.Now(),
		}
		if err := db.Create(&o).Error; err != nil {
			t.Fatal(err)
		}
	}
	// 20 governed actions for the user so the min-evidence gate passes.
	for i := 0; i < 20; i++ {
		db.Create(&models.ActionEnvelope{OrganizationID: "org1", UserID: "u2", ActionID: "a" + string(rune('a'+i)), ActionType: "tool_use", OccurredAt: time.Now().UTC().Format(time.RFC3339)})
	}
	scores, err := svc.GeneratePeriodScores("org1", period.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(scores) != 1 || !scores[0].Sufficient {
		t.Fatalf("expected 1 sufficient score, got %+v", scores)
	}
	// 7 objective rows but only 5 distinct (4 + 1 split coalesced) accepted.
	if scores[0].AcceptedOutcomes != 5 {
		t.Fatalf("coalesce failed: expected 5 accepted outcomes, got %d", scores[0].AcceptedOutcomes)
	}
}

// User scoping: only that user's objectives/actions/findings count, never the
// whole org (defect fix from the prototype).
func TestUserScoping(t *testing.T) {
	db := lbDB(t)
	svc := New(db)
	period := testPeriod()
	db.Create(&period)
	// u3 has 6 accepted; u4 has 2. Org totals differ per user.
	for i := 1; i <= 6; i++ {
		seedObjective(db, "u3o"+string(rune('a'+i)), "u3", t, "accepted")
	}
	for i := 1; i <= 2; i++ {
		seedObjective(db, "u4o"+string(rune('a'+i)), "u4", t, "accepted")
	}
	// Actions: only u3 gets 20+ governed actions.
	for i := 0; i < 25; i++ {
		db.Create(&models.ActionEnvelope{OrganizationID: "org1", UserID: "u3", ActionID: "c3" + string(rune('a'+i)), EnvelopeDigest: "x" + string(rune('a'+i)), ActionType: "tool_use", OccurredAt: time.Now().UTC().Format(time.RFC3339)})
	}
	scores, _ := svc.GeneratePeriodScores("org1", period.ID)
	byOwner := map[string]models.ActionEnvelope{}
	_ = byOwner
	if len(scores) != 2 {
		t.Fatalf("expected 2 subjects, got %d", len(scores))
	}
	var u3sufficient, u4sufficient bool
	for _, s := range scores {
		if s.AcceptedOutcomes == 6 {
			u3sufficient = s.Sufficient
		}
		if s.AcceptedOutcomes == 2 {
			u4sufficient = s.Sufficient
		}
	}
	if !u3sufficient {
		t.Fatalf("u3 (6 outcomes, 25 actions) should be sufficient: %+v", scores)
	}
	if u4sufficient {
		t.Fatalf("u4 (2 outcomes) incorrectly sufficient (scoping leak): %+v", scores)
	}
}

// Critical confirmed violation caps overall via the Patty-enforced ceiling.
func TestCriticalCeiling(t *testing.T) {
	db := lbDB(t)
	svc := New(db)
	period := testPeriod()
	db.Create(&period)
	for i := 1; i <= 6; i++ {
		seedObjective(db, "c5o"+string(rune('a'+i)), "u5", t, "accepted")
	}
	for i := 0; i < 25; i++ {
		db.Create(&models.ActionEnvelope{OrganizationID: "org1", UserID: "u5", ActionID: "ce" + string(rune('a'+i)), ActionType: "tool_use", OccurredAt: time.Now().UTC().Format(time.RFC3339)})
	}
	// One critical finding via u5's session.
	db.Create(&models.Session{AuditBase: models.AuditBase{OrganizationID: "org1"}, UserID: "u5", HarnessID: "h-u5", SessionID: "ses-u5", Status: "active"})
	db.Create(&models.SecurityFinding{OrganizationID: "org1", SessionID: "ses-u5", FindingType: "secret_exposure", Severity: "critical", Status: "open", OccurredAt: time.Now().UTC().Format(time.RFC3339)})
	scores, _ := svc.GeneratePeriodScores("org1", period.ID)
	if len(scores) != 1 {
		t.Fatalf("expected 1 subject, got %d", len(scores))
	}
	if scores[0].Overall > DefaultRubric.CriticalCeiling {
		t.Fatalf("critical violation ceiling not enforced: overall %.1f > %.0f", scores[0].Overall, DefaultRubric.CriticalCeiling)
	}
	if scores[0].ConfirmedViolations != 1 {
		t.Fatalf("critical violation not counted: %+v", scores[0])
	}
}

// rankCohort: eligible (sufficient) subjects always rank ahead of insufficient
// ones; within eligible class, higher overall wins; insufficient carry no rank.
func TestRankCohortInvariant(t *testing.T) {
	mk := func(overall float64, sufficient bool) *Score { return &Score{Overall: overall, Sufficient: sufficient} }
	list := []*Score{
		mk(20, true), mk(90, true), mk(30, false), mk(70, true),
	}
	rankCohort(list)
	// eligible first, descending overall: 90,70,20 then insufficient 30.
	wantOrder := []float64{90, 70, 20, 30}
	for i, sc := range list {
		if sc.Overall != wantOrder[i] {
			t.Fatalf("order[%d]=%v want %v", i, sc.Overall, wantOrder[i])
		}
	}
	if list[0].Rank != 1 || list[1].Rank != 2 || list[2].Rank != 3 || list[2].Percentile <= 0 {
		t.Fatalf("eligible ranks wrong: %+v", list)
	}
	if list[3].Rank != 0 || list[3].Percentile != 0 {
		t.Fatalf("insufficient must carry no rank/percentile: %+v", list[3])
	}
	// Percentile takes the full cohort size into account (not only eligible).
	if list[0].Percentile != 100 || list[2].Percentile < 50 {
		t.Fatalf("percentile bounds wrong: %+v", list)
	}
}
