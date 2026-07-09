package metric

type UsageRecord struct {
	PromptTokens     int
	CacheHitTokens   int
	CompletionTokens int
	PricePerMInput   float64
	PricePerMCache   float64
	PricePerMOutput  float64
}

func EstimateCost(u UsageRecord) float64 {
	nonCachePrompt := u.PromptTokens - u.CacheHitTokens
	if nonCachePrompt < 0 {
		nonCachePrompt = 0
	}
	promptCost := float64(nonCachePrompt) * u.PricePerMInput / 1_000_000
	cacheCost := float64(u.CacheHitTokens) * u.PricePerMCache / 1_000_000
	completionCost := float64(u.CompletionTokens) * u.PricePerMOutput / 1_000_000
	return promptCost + cacheCost + completionCost
}
