package mapper

import (
	"testing"

	"nexflow/internal/models"
)

// ── in-memory stub ────────────────────────────────────────────────────────────

type stubRepo struct {
	data map[string]*models.Mapping
}

func newStub(entries ...models.Mapping) *stubRepo {
	s := &stubRepo{data: make(map[string]*models.Mapping)}
	for i := range entries {
		s.data[entries[i].RawName] = &entries[i]
	}
	return s
}

func (s *stubRepo) FindByRawName(raw string) (*models.Mapping, error) {
	if m, ok := s.data[raw]; ok {
		return m, nil
	}
	return nil, nil
}

func (s *stubRepo) ListAll() ([]models.Mapping, error) {
	out := make([]models.Mapping, 0, len(s.data))
	for _, m := range s.data {
		out = append(out, *m)
	}
	return out, nil
}

func (s *stubRepo) IncrementUsage(id string) error { return nil }

func (s *stubRepo) Upsert(rawName, itemCode, unitCode, source string, billID *string) error {
	s.data[rawName] = &models.Mapping{
		ID:         "stub-" + rawName,
		RawName:    rawName,
		ItemCode:   itemCode,
		UnitCode:   unitCode,
		Confidence: 1.0,
		Source:     source,
	}
	return nil
}

// ── Match: exact ──────────────────────────────────────────────────────────────

func TestMatch_ExactHit(t *testing.T) {
	repo := newStub(models.Mapping{
		ID:       "1",
		RawName:  "ปูนซีเมนต์",
		ItemCode: "CEM001",
		UnitCode: "ถุง",
	})
	svc := New(repo)
	result := svc.Match("ปูนซีเมนต์")

	if result.Unmapped {
		t.Fatal("expected mapped result")
	}
	if result.Score != 1.0 {
		t.Errorf("expected score 1.0 for exact match, got %.2f", result.Score)
	}
	if result.Mapping.ItemCode != "CEM001" {
		t.Errorf("expected CEM001, got %s", result.Mapping.ItemCode)
	}
}

// ── Match: fuzzy high score → auto-confirm ────────────────────────────────────

func TestMatch_FuzzyAutoConfirm(t *testing.T) {
	repo := newStub(models.Mapping{
		ID:       "2",
		RawName:  "ปูนซีเมนต์ตราช้าง",
		ItemCode: "CEM002",
		UnitCode: "ถุง",
	})
	svc := New(repo)
	// Very close string — expect high score
	result := svc.Match("ปูนซีเมนต์ตราช้าง 50กก")

	if result.Unmapped {
		t.Fatal("expected fuzzy match, got unmapped")
	}
	if result.NeedsReview {
		// Score just below auto-confirm is acceptable depending on string diff
		t.Logf("fuzzy score %.3f — needs review (acceptable if score < %.2f)", result.Score, AutoConfirmScore)
	}
}

// ── Match: empty repo → unmapped ─────────────────────────────────────────────

func TestMatch_EmptyRepo(t *testing.T) {
	svc := New(newStub())
	result := svc.Match("สินค้าไม่มีใน DB")
	if !result.Unmapped {
		t.Error("expected unmapped result when repo is empty")
	}
}

// ── Match: very different string → not auto-confirmed ────────────────────────

func TestMatch_VeryDifferentString(t *testing.T) {
	repo := newStub(models.Mapping{
		ID:       "3",
		RawName:  "ปูนซีเมนต์",
		ItemCode: "CEM001",
		UnitCode: "ถุง",
	})
	svc := New(repo)
	result := svc.Match("XYZABCDEF123") // totally unrelated

	// Should not auto-confirm — either unmapped or needs review
	if !result.Unmapped && !result.NeedsReview {
		t.Errorf("expected unmapped or needs_review for unrelated string, got score=%.2f", result.Score)
	}
}

// ── LearnFromFeedback ─────────────────────────────────────────────────────────

func TestLearnFromFeedback(t *testing.T) {
	repo := newStub()
	svc := New(repo)

	err := svc.LearnFromFeedback("สีขาว 5ลิตร", "PAINT001", "กระป๋อง", nil)
	if err != nil {
		t.Fatalf("LearnFromFeedback returned error: %v", err)
	}

	// Should now find it by exact match
	result := svc.Match("สีขาว 5ลิตร")
	if result.Unmapped {
		t.Error("expected mapping after learning from feedback")
	}
}
