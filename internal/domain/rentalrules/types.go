package rentalrules

type Status string

const (
	StatusSuccess      Status = "success"
	StatusInsufficient Status = "insufficient_knowledge"
)

type Input struct {
	EvidenceText string
}

type Rule struct {
	ID                   string
	Category             string
	Title                string
	Guidance             string
	Scope                string
	Source               string
	VerificationRequired bool
}

type Result struct {
	Status         Status
	Message        string
	CatalogVersion string
	Rules          []Rule
}
