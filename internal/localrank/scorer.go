package localrank

import (
	"math"
	"sort"
	"strings"

	"github.com/zxq97/agent/api/guide"
	"github.com/zxq97/agent/internal/searchplan"
	"github.com/zxq97/agent/internal/vehiclefacts"
)

const MinimumCoverage = 0.5

type quoteScore struct {
	index    int
	score    float64
	coverage float64
}

// Report describes what the deterministic scenario scorer actually used.
type Report struct {
	Applied                bool
	ModelVersions          []string
	EvidenceByRequirement  map[string][]string
	CoverageByRequirement  map[string]float64
	UnscoredRequirementIDs []string
}

// Rank applies only approved versioned scoring models to the fetched set.
// Missing facts contribute no score and reduce coverage.
func Rank(values []guide.VehRate, ranks []searchplan.ExploratoryRank) ([]guide.VehRate, Report) {
	result := append([]guide.VehRate(nil), values...)
	report := Report{
		EvidenceByRequirement: make(map[string][]string),
		CoverageByRequirement: make(map[string]float64),
	}
	if len(values) == 0 || len(ranks) == 0 {
		return result, report
	}
	scores := make([]quoteScore, len(values))
	for index := range values {
		scores[index].index = index
	}
	for _, rank := range ranks {
		var coverageTotal float64
		applied := false
		evidence := make(map[string]struct{})
		for index, value := range values {
			score, coverage, facts := scenarioScore(value, rank.ScenarioID)
			coverageTotal += coverage
			if coverage < MinimumCoverage {
				continue
			}
			applied = true
			scores[index].score += score * rank.Weight * coverage
			scores[index].coverage += coverage
			for _, fact := range facts {
				evidence[fact] = struct{}{}
			}
		}
		report.CoverageByRequirement[rank.RequirementID] = coverageTotal / float64(len(values))
		if !applied {
			report.UnscoredRequirementIDs = append(report.UnscoredRequirementIDs, rank.RequirementID)
			continue
		}
		report.Applied = true
		report.ModelVersions = appendUnique(report.ModelVersions, rank.ModelVersion)
		report.EvidenceByRequirement[rank.RequirementID] = sortedKeys(evidence)
	}
	if !report.Applied {
		return result, report
	}
	sort.SliceStable(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].coverage > scores[j].coverage
		}
		return scores[i].score > scores[j].score
	})
	ranked := make([]guide.VehRate, 0, len(result))
	for _, score := range scores {
		ranked = append(ranked, result[score.index])
	}
	return ranked, report
}

func scenarioScore(value guide.VehRate, scenario string) (float64, float64, []string) {
	if value.Vehicle == nil {
		return 0, 0, nil
	}
	facts := vehiclefacts.FromGuide(value)
	seats := 0
	if facts.Seats.Available {
		seats = int(facts.Seats.Value)
	}
	bodyStyle := strings.TrimSpace(facts.GroupName.Value + " " + facts.VehicleName.Value)
	switch scenario {
	case "elderly_friendly_v1":
		return weightedScore([]factor{
			numericRangeFactor(seats, 5, 7, 0.65, "座位数"),
			bodyStyleFactor(bodyStyle, []string{"mpv", "suv", "商务"}, 0.35),
		})
	case "family_trip_v1":
		return weightedScore([]factor{
			numericRangeFactor(seats, 5, 7, 0.60, "座位数"),
			bodyStyleFactor(bodyStyle, []string{"mpv", "suv", "旅行"}, 0.40),
		})
	case "large_space_v1":
		return weightedScore([]factor{
			numericAtLeastFactor(seats, 5, 0.55, "座位数"),
			bodyStyleFactor(bodyStyle, []string{"mpv", "suv", "商务", "旅行"}, 0.45),
		})
	case "long_distance_v1":
		return weightedScore([]factor{
			numericRangeFactor(seats, 4, 7, 0.40, "座位数"),
			bodyStyleFactor(bodyStyle, []string{"中型", "大型", "suv", "旅行"}, 0.60),
		})
	case "large_luggage_v1":
		return weightedScore([]factor{
			bodyStyleFactor(bodyStyle, []string{"mpv", "suv", "旅行", "商务"}, 0.60),
			numericAtLeastFactor(seats, 5, 0.40, "座位数"),
		})
	default:
		return 0, 0, nil
	}
}

type factor struct {
	available bool
	score     float64
	weight    float64
	evidence  string
}

func weightedScore(values []factor) (float64, float64, []string) {
	var score, availableWeight, totalWeight float64
	var evidence []string
	for _, value := range values {
		totalWeight += value.weight
		if !value.available {
			continue
		}
		availableWeight += value.weight
		score += value.score * value.weight
		if value.evidence != "" {
			evidence = append(evidence, value.evidence)
		}
	}
	if totalWeight == 0 || availableWeight == 0 {
		return 0, 0, nil
	}
	return score / availableWeight, availableWeight / totalWeight, evidence
}

func numericRangeFactor(value, minimum, maximum int, weight float64, label string) factor {
	if value <= 0 {
		return factor{weight: weight}
	}
	distance := 0
	if value < minimum {
		distance = minimum - value
	} else if value > maximum {
		distance = value - maximum
	}
	return factor{
		available: true,
		score:     math.Max(0, 1-float64(distance)*0.25),
		weight:    weight,
		evidence:  label,
	}
}

func numericAtLeastFactor(value, minimum int, weight float64, label string) factor {
	if value <= 0 {
		return factor{weight: weight}
	}
	score := math.Min(1, float64(value)/float64(max(minimum, 1)))
	return factor{available: true, score: score, weight: weight, evidence: label}
}

func bodyStyleFactor(value string, keywords []string, weight float64) factor {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return factor{weight: weight}
	}
	score := 0.3
	for _, keyword := range keywords {
		if strings.Contains(value, strings.ToLower(keyword)) {
			score = 1
			break
		}
	}
	return factor{available: true, score: score, weight: weight, evidence: "车辆名称/车型组"}
}

func appendUnique(values []string, target string) []string {
	for _, value := range values {
		if value == target {
			return values
		}
	}
	return append(values, target)
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
