package maps

// SearchRequest contains the user's raw location expression.
// The Maps client always searches nationwide.
type SearchRequest struct {
	Keyword string `json:"keyword"`
	Limit   int    `json:"limit,omitempty"`
}

// SearchResponse is the normalized result returned from a map provider.
type SearchResponse struct {
	Candidates []Candidate `json:"candidates"`
}

// Candidate is a selectable location returned by a map provider. ID is
// provider-owned and must never be supplied by an LLM.
type Candidate struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Address   string  `json:"address,omitempty"`
	CityID    int     `json:"city_id"`
	CityName  string  `json:"city_name,omitempty"`
	Latitude  float64 `json:"latitude,omitempty"`
	Longitude float64 `json:"longitude,omitempty"`
}
