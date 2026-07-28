// Package requirement owns transport-independent user requirement semantics.
// It deliberately does not depend on Guide menus, filter codes, or search
// execution modes.
package requirement

type Category string

const (
	CategoryVehicle       Category = "vehicle"
	CategoryPrice         Category = "price"
	CategoryConfiguration Category = "configuration"
	CategoryPreference    Category = "preference"
	CategoryUsageScenario Category = "usage_scenario"
	CategoryUnknown       Category = "unknown"
)

type ValueKind string

const (
	ValueNone   ValueKind = "none"
	ValueText   ValueKind = "text"
	ValueNumber ValueKind = "number"
	ValueRange  ValueKind = "range"
	ValueEntity ValueKind = "entity"
)

type NumberRange struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

// Value is a tagged union. Only the field selected by Kind may be populated.
// Provider or catalog entity IDs are added by deterministic normalization, not
// accepted from LLM extraction output.
type Value struct {
	Kind   ValueKind    `json:"kind"`
	Text   string       `json:"text,omitempty"`
	Number *float64     `json:"number,omitempty"`
	Range  *NumberRange `json:"range,omitempty"`
	Entity *EntityRef   `json:"entity,omitempty"`
	Unit   string       `json:"unit,omitempty"`
}

type EntityRef struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Name     string `json:"name"`
	BrandID  string `json:"brand_id,omitempty"`
	ParentID string `json:"parent_id,omitempty"`
}
