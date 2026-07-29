package agenthub

// RecallRequest asks AgentHub for bounded vehicle-entity candidates.
type RecallRequest struct {
	Query          string `json:"query"`
	EntityType     string `json:"entity_type"`
	BrandHint      string `json:"brand_hint,omitempty"`
	SeriesHint     string `json:"series_hint,omitempty"`
	CatalogVersion string `json:"catalog_version"`
	TopK           int    `json:"top_k"`
}

// RecallCandidate is untrusted recall evidence. Callers must revalidate its
// name and relationships against the authoritative local catalog.
type RecallCandidate struct {
	CandidateID   string  `json:"candidate_id"`
	Name          string  `json:"name"`
	EntityType    string  `json:"entity_type"`
	BrandHint     string  `json:"brand_hint,omitempty"`
	SeriesHint    string  `json:"series_hint,omitempty"`
	Evidence      string  `json:"evidence,omitempty"`
	RecallScore   float64 `json:"recall_score,omitempty"`
	SourceVersion string  `json:"source_version,omitempty"`
}

type RecallResponse struct {
	Candidates []RecallCandidate `json:"candidates"`
}
