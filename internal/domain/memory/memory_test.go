package memory

import "testing"

func TestJobStatusConstants(t *testing.T) {
	cases := []struct {
		name string
		got  JobStatus
		want string
	}{
		{"pending", JobPending, "pending"},
		{"processing", JobProcessing, "processing"},
		{"ready", JobReady, "ready"},
		{"failed", JobFailed, "failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if string(tc.got) != tc.want {
				t.Errorf("got %q, want %q", tc.got, tc.want)
			}
		})
	}
}

func TestTierConstants(t *testing.T) {
	if string(TierStore) != "store" {
		t.Errorf("TierStore = %q, want %q", TierStore, "store")
	}
	if string(TierCache) != "cache" {
		t.Errorf("TierCache = %q, want %q", TierCache, "cache")
	}
}

func TestCompileJobStruct(t *testing.T) {
	job := CompileJob{
		ID:        "job-1",
		Source:    "https://example.com/doc",
		Status:    JobPending,
		Progress:  0,
		Total:     10,
		CreatedAt: "2026-09-16T00:00:00Z",
		UpdatedAt: "2026-09-16T00:00:00Z",
	}

	if job.ID != "job-1" {
		t.Errorf("ID = %q", job.ID)
	}
	if job.Source != "https://example.com/doc" {
		t.Errorf("Source = %q", job.Source)
	}
	if job.Status != JobPending {
		t.Errorf("Status = %q", job.Status)
	}
	if job.Total != 10 {
		t.Errorf("Total = %d", job.Total)
	}
	if job.Error != "" {
		t.Errorf("Error = %q, want empty", job.Error)
	}
}

func TestCompileOptionsDefaults(t *testing.T) {
	opts := CompileOptions{
		RequireApproval: false,
		MaxChunkTokens:  512,
		BatchSize:       100,
	}

	if opts.RequireApproval {
		t.Error("RequireApproval should be false by default")
	}
	if opts.MaxChunkTokens != 512 {
		t.Errorf("MaxChunkTokens = %d", opts.MaxChunkTokens)
	}
	if opts.BatchSize != 100 {
		t.Errorf("BatchSize = %d", opts.BatchSize)
	}
}

func TestEntryStruct(t *testing.T) {
	e := Entry{
		ID:          1,
		Content:     "test content",
		Source:      "test",
		Tags:        []string{"go", "test"},
		Importance:  0.8,
		AccessCount: 5,
		Tier:        TierStore,
		CreatedAt:   "2026-09-16T00:00:00Z",
	}

	if e.ID != 1 {
		t.Errorf("ID = %d", e.ID)
	}
	if e.Content != "test content" {
		t.Errorf("Content = %q", e.Content)
	}
	if len(e.Tags) != 2 {
		t.Errorf("Tags len = %d", len(e.Tags))
	}
	if e.Tier != TierStore {
		t.Errorf("Tier = %q", e.Tier)
	}
	if e.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil for store tier")
	}
}

func TestHitStruct(t *testing.T) {
	h := Hit{
		Entry: Entry{
			ID:      1,
			Content: "hit content",
			Tier:    TierCache,
		},
		Score:     0.95,
		MatchType: "both",
	}

	if h.ID != 1 {
		t.Errorf("ID = %d", h.ID)
	}
	if h.Score != 0.95 {
		t.Errorf("Score = %f", h.Score)
	}
	if h.MatchType != "both" {
		t.Errorf("MatchType = %q", h.MatchType)
	}
}

func TestSearchQueryStruct(t *testing.T) {
	q := SearchQuery{
		Text:        "how to test",
		Tier:        TierStore,
		TopK:        5,
		MinScore:    0.7,
		MaxTokens:   1000,
		QueryVector: []float32{0.1, 0.2, 0.3},
	}

	if q.Text != "how to test" {
		t.Errorf("Text = %q", q.Text)
	}
	if q.Tier != TierStore {
		t.Errorf("Tier = %q", q.Tier)
	}
	if q.TopK != 5 {
		t.Errorf("TopK = %d", q.TopK)
	}
	if q.MinScore != 0.7 {
		t.Errorf("MinScore = %f", q.MinScore)
	}
	if len(q.QueryVector) != 3 {
		t.Errorf("QueryVector len = %d", len(q.QueryVector))
	}
}

func TestSearchQueryEmptyVector(t *testing.T) {
	q := SearchQuery{
		Text:        "fallback search",
		Tier:        TierStore,
		TopK:        3,
		QueryVector: nil,
	}

	if q.QueryVector != nil {
		t.Error("QueryVector should be nil for FTS5 fallback")
	}
}

func TestProjectionStruct(t *testing.T) {
	p := Projection{
		Pinned:    []string{"system prompt"},
		Retrieved: []Hit{{Entry: Entry{ID: 1}, Score: 0.9}},
		Recent:    []string{"user message"},
		Summary:   "conversation summary",
		Usage: TokenUsage{
			Pinned:    100,
			Retrieved: 200,
			Recent:    50,
			Total:     350,
			Budget:    4000,
		},
	}

	if len(p.Pinned) != 1 {
		t.Errorf("Pinned len = %d", len(p.Pinned))
	}
	if len(p.Retrieved) != 1 {
		t.Errorf("Retrieved len = %d", len(p.Retrieved))
	}
	if p.Usage.Total != 350 {
		t.Errorf("Usage.Total = %d", p.Usage.Total)
	}
}

func TestTokenUsageStruct(t *testing.T) {
	u := TokenUsage{
		Pinned:    500,
		Retrieved: 1000,
		Recent:    200,
		Total:     1700,
		Budget:    4096,
	}

	if u.Pinned+u.Retrieved+u.Recent != u.Total {
		t.Errorf("sum != total: %d+%d+%d != %d", u.Pinned, u.Retrieved, u.Recent, u.Total)
	}
}

func TestPreferenceStruct(t *testing.T) {
	p := Preference{
		Key:         "language",
		Value:       "Go",
		Probability: 0.85,
		EvidenceCnt: 10,
		LastHitAt:   "2026-09-16T00:00:00Z",
	}

	if p.Key != "language" {
		t.Errorf("Key = %q", p.Key)
	}
	if p.Value != "Go" {
		t.Errorf("Value = %q", p.Value)
	}
	if p.Probability != 0.85 {
		t.Errorf("Probability = %f", p.Probability)
	}
	if p.EvidenceCnt != 10 {
		t.Errorf("EvidenceCnt = %d", p.EvidenceCnt)
	}
}

func TestDistillOptionsStruct(t *testing.T) {
	opts := DistillOptions{
		MinEvidence:         3,
		ImportanceThreshold: 0.6,
		WindowDays:          7,
	}

	if opts.MinEvidence != 3 {
		t.Errorf("MinEvidence = %d", opts.MinEvidence)
	}
	if opts.ImportanceThreshold != 0.6 {
		t.Errorf("ImportanceThreshold = %f", opts.ImportanceThreshold)
	}
	if opts.WindowDays != 7 {
		t.Errorf("WindowDays = %d", opts.WindowDays)
	}
}

func TestDistillResultStruct(t *testing.T) {
	r := DistillResult{Extracted: 5}
	if r.Extracted != 5 {
		t.Errorf("Extracted = %d", r.Extracted)
	}
}
