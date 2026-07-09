package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/zxq97/rental-agent/internal/llm"
	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/prompt"
	"github.com/zxq97/rental-agent/internal/tools"
	"github.com/zxq97/rental-agent/internal/tyche"
	"github.com/zxq97/rental-agent/internal/types"
)

// SearchCapability 导购搜车:
//  1. 取还车 POI 解析(MCP)
//  2. 需求合并 + 生成 filter_codes
//  3. 调 guide/store/list/agent 搜报价(带 filter_codes)
//  4. 写 state(Constraints/LastSearch/CachedMenu/LastQuotes)
//  5. LLM 生成引导语
type SearchCapability struct{}

func (c *SearchCapability) Name() string { return "search" }

func (c *SearchCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	st := in.State
	logger := in.Deps.Logger

	// 1. 取还车地点必须已确定。
	//    a) 若 decide 抽到了新的 pickup_text,写入 slot 供本轮/下轮解析。
	//    b) 已有真实 PickupPOI → 直接跳过。
	//    c) 无真实 POI 且无 pickup_text → 前置反问,绝不拿用户需求原话当地点关键词。
	//    d) 有 pickup_text 但没真实 POI → 走 MCP 解析,失败也回退成反问。
	if in.Decision != nil && in.Decision.PickupText != "" {
		st.Slot.PickupText = in.Decision.PickupText
	}
	applyDecisionRentalTimes(st, in.Decision)
	var poi poiData
	if !st.Rental.PickupPOI.Valid() {
		if st.Slot.PickupText == "" {
			logf(logger, "[search] stage=capability:search gate=pickup_missing action=ask_location")
			return &CapabilityResult{
				Clarification: rentalMissingClarification(st),
			}, nil
		}
		logf(logger, "[search] stage=capability:search step=resolve_poi pickup_text=%q", st.Slot.PickupText)
		p, err := c.resolvePickupDropoff(ctx, in)
		if err != nil {
			logf(logger, "[search] stage=capability:search step=resolve_poi status=error err=%v pickup_text=%q", err, st.Slot.PickupText)
			// 解析失败:清掉误存的 PickupText,让下一轮重新问,不硬搜。
			st.Slot.PickupText = ""
			return &CapabilityResult{
				Clarification: &Clarification{
					Question: "这个取车地点我没查到,能换个更具体的说法吗?比如'首都机场T3'/'北京西站'/'朝阳区望京'。",
					Slot:     "pickup_location",
				},
			}, nil
		}
		poi = p
		logf(logger, "[search] stage=capability:search step=resolve_poi status=ok city=%d name=%s lat=%.4f lng=%.4f", poi.CityID, poi.Name, poi.Latitude, poi.Longitude)
	}
	if clar := rentalMissingClarification(st); clar != nil {
		logf(logger, "[search] stage=capability:search gate=rental_incomplete slot=%s action=ask", clar.Slot)
		return &CapabilityResult{Clarification: clar}, nil
	}

	// 2. 需求合并:会话累积 + 本轮增量 + 冲突衰减 + 活跃过滤
	st.TurnCount++
	turn := st.TurnCount
	ApplySceneKnowledgeToDecision(in.Decision, in.UserInput)
	gate := ApplyConfidenceGate(in.Decision, nil)
	if gate.Action == ConfidenceActionInterpret {
		in.Decision = InterpretFilterIfNeeded(ctx, FilterInterpretInput{
			Factory:  in.Factory,
			Decision: in.Decision,
			UserText: in.UserInput,
			State:    st,
			Reason:   gate.Reason,
		})
		gate = ApplyConfidenceGate(in.Decision, nil)
	}
	if gate.Action == ConfidenceActionAsk {
		return &CapabilityResult{Clarification: &Clarification{
			Question: "我再确认一个关键点,你更看重价格、空间还是车新?",
			Options:  []string{"价格更低", "空间更大", "车况更新"},
			Slot:     "search_preference",
		}}, nil
	}
	if len(gate.NormalizedDelta) > 0 {
		in.Decision.NeedDelta = gate.NormalizedDelta
	}
	logf(logger, "[search] confidence_action=%s reason=%s", gate.Action, gate.Reason)

	sessionNeeds := BuildNeedsFromConstraints(st.Constraints)
	merged := ApplyDelta(TickNeeds(sessionNeeds, turn), in.Decision.NeedDelta, turn)
	merged = ApplyConflictDecay(merged, in.Decision.NeedDelta)
	active := FilterActiveNeeds(merged)
	logf(logger, "[search] needs: session=%d delta=%d merged=%d active=%d", len(sessionNeeds), len(in.Decision.NeedDelta), len(merged), len(active))

	// 3. 生成 filter_codes(静态映射,有菜单时做白名单校验)
	filterCodes, uncovered := StaticRecall(active, st.CachedMenu)
	iteration := ApplyIterationPolicy(in.Decision, st, filterCodes)
	logf(logger, "[search] filter_codes=%v uncovered=%d", filterCodes, len(uncovered))

	// 4. 搜报价
	quotes, guideResp, ok := c.searchQuotesViaGuide(ctx, in, poi, iteration)
	if !ok || len(quotes) == 0 {
		logf(logger, "[search] no quotes found")
		st.Constraints = UpdateConstraints(merged)
		return &CapabilityResult{Text: "抱歉,这个条件暂时没找到合适的车,换个地点或时间再试试?"}, nil
	}
	quotes = filterExcludedQuotes(quotes, iteration.ExcludedRefs)
	quotes = filterNegativeNeedQuotes(quotes, merged)
	logf(logger, "[search] found %d quotes", len(quotes))

	// 5. 写 state
	ctxID := ""
	if guideResp != nil {
		ctxID = guideResp.ContextID
		// 缓存菜单
		st.CachedMenu = extractMenuViews(guideResp.MenuGroup)
	}

	refs := make([]orchestration.QuoteRef, 0, len(quotes))
	for i, q := range quotes {
		refs = append(refs, orchestration.QuoteRef{
			ReferenceID: q.ReferenceID, Supplier: q.Supplier,
			CarName: q.CarName, BrandName: q.BrandName,
			DailyPrice: q.DailyPrice, TotalPrice: q.TotalPrice,
			Index: i + 1,
		})
	}
	st.SetQuotes(ctxID, refs)
	st.Constraints = UpdateConstraints(merged)
	st.LastSearch = &types.LastSearchState{
		SearchMode:   iteration.SearchMode,
		FilterCodes:  iteration.FilterCodes,
		Page:         iteration.Page,
		PageSize:     iteration.PageSize,
		HasMore:      len(quotes) >= iteration.PageSize,
		ShownRefs:    quoteRefs(refs),
		ExcludedRefs: iteration.ExcludedRefs,
		MinPrice:     minQuotePrice(quotes),
		MaxPrice:     maxQuotePrice(quotes),
		RelaxLevel:   iteration.RelaxLevel,
	}

	// state 写完再拍一张,便于对比 pipeline_start 与 search_written 的差异。
	if logger != nil {
		fmt.Fprintln(logger, orchestration.SummarizeForLog(st, "search_written"))
	}

	// 6. LLM 生成引导语
	summary := summarizeQuotes(quotes)
	guide := c.streamGuide(ctx, in, summary)

	return &CapabilityResult{
		Text:       guide,
		ToolName:   tools.ToolSearchQuotes,
		ToolArgs:   fmt.Sprintf(`{"filter_codes":%s,"page":%d}`, mustJSON(iteration.FilterCodes), iteration.Page),
		ToolResult: fmt.Sprintf("已为用户展示 %d 辆候选: %s", len(quotes), summary),
	}, nil
}

// searchQuotesViaGuide 通过 guide/store/list/agent 搜报价(带 filter_codes)。
// 报价只走 rental-guide;guide 未配置或调用失败一律视为空结果,不再 fallback 到已下线的
// MCP rental_search_quotes(该工具已从白名单移除,即便调用也会被拒)。
func (c *SearchCapability) searchQuotesViaGuide(ctx context.Context, in CapabilityInput, poi poiData, iteration IterationPlan) ([]quoteItem, *tyche.GuideSearchResponse, bool) {
	guide := in.Deps.Guide
	if guide == nil {
		if in.Deps.Logger != nil {
			fmt.Fprintf(in.Deps.Logger, "[search] stage=capability:search status=no_guide_client action=skip_search\n")
		}
		return nil, nil, false
	}

	pickup := &tyche.GuideRentalInfo{
		CityID:       in.State.Rental.PickupCityID,
		LocationName: in.State.Rental.PickupName,
		DateTime:     rentalTimeForGuide(in.State.Rental.PickupTime),
	}
	dropoff := &tyche.GuideRentalInfo{
		CityID:       in.State.Rental.DropoffCityID,
		LocationName: in.State.Rental.DropoffName,
		DateTime:     rentalTimeForGuide(in.State.Rental.DropoffTime),
	}
	attachGuidePOI(pickup, in.State.Rental.PickupPOI)
	dropoffPOI := in.State.Rental.DropoffPOI
	if !dropoffPOI.Valid() && in.State.Rental.DropoffCityID == in.State.Rental.PickupCityID && in.State.Rental.DropoffName == in.State.Rental.PickupName {
		dropoffPOI = in.State.Rental.PickupPOI
	}
	attachGuidePOI(dropoff, dropoffPOI)
	// 兼容本轮刚解析、但 state 还没完整迁移的旧路径。
	if pickup.POI == nil && poi.Latitude != 0 {
		pickup.POI = &tyche.GuideRentalInfoPOI{Latitude: poi.Latitude, Longitude: poi.Longitude}
	}
	if dropoff.POI == nil && poi.Latitude != 0 {
		dropoff.POI = &tyche.GuideRentalInfoPOI{Latitude: poi.Latitude, Longitude: poi.Longitude}
	}

	req := tyche.GuideSearchRequest{
		PickupRentalInfo:  pickup,
		DropoffRentalInfo: dropoff,
		Filter:            tyche.GuideFilterInfo{FilterCodes: iteration.FilterCodes},
		Page:              iteration.Page,
		PageSize:          iteration.PageSize,
	}
	if in.State.Rental.ContextID != "" {
		req.ContextID = in.State.Rental.ContextID
	}

	resp, err := guide.SearchQuotes(ctx, req)
	if err != nil {
		if in.Deps.Logger != nil {
			fmt.Fprintf(in.Deps.Logger, "[search] stage=capability:search status=guide_error err=%v (no fallback)\n", err)
		}
		return nil, nil, false
	}

	quotes := convertGuideVehRates(resp.VehRates)
	return quotes, resp, len(quotes) > 0
}

// convertGuideVehRates 把 guide veh_rates 转成统一 quoteItem。
func convertGuideVehRates(rates []tyche.GuideVehRate) []quoteItem {
	quotes := make([]quoteItem, 0, len(rates))
	for _, r := range rates {
		if r.Vehicle == nil {
			continue
		}
		q := quoteItem{
			CarName:    r.Vehicle.VehicleName,
			BrandName:  r.Vehicle.BrandName,
			CarType:    r.Vehicle.GroupName,
			Seats:      r.Vehicle.Seats,
			FuelType:   fuelTypeString(r.Vehicle.FuelType),
			Supplier:   r.SupplierCode,
			DailyPrice: r.DailyDeductionAmount,
		}
		if r.ReferenceInfo != nil {
			q.ReferenceID = r.ReferenceInfo.ReferenceID
		}
		if r.TotalCharge != nil {
			q.TotalPrice = r.TotalCharge.DeductionAmount
		}
		quotes = append(quotes, q)
	}
	return quotes
}

func fuelTypeString(ft int) string {
	switch ft {
	case 1:
		return "汽油"
	case 2:
		return "柴油"
	case 3:
		return "混动"
	case 4:
		return "纯电"
	default:
		return "其他"
	}
}

// extractMenuViews 从 guide 返回的菜单提取精简视图,供 StaticRecall 白名单校验。
func extractMenuViews(groups []tyche.GuideMenuGroup) []types.MenuGroupView {
	if len(groups) == 0 {
		return nil
	}
	views := make([]types.MenuGroupView, 0, len(groups))
	for _, g := range groups {
		if !strings.HasPrefix(g.GroupCode, "filter/") {
			continue
		}
		var items []types.MenuItemView
		for _, gi := range g.GroupItems {
			for _, item := range gi.Items {
				items = append(items, types.MenuItemView{
					ItemCode: item.ItemCode,
					Name:     item.Name,
				})
			}
		}
		views = append(views, types.MenuGroupView{
			GroupCode: g.GroupCode,
			GroupName: g.Name,
			Items:     items,
		})
	}
	return views
}

// (已删除 searchQuotesMCP:rental_search_quotes 工具下线,报价只走 rental-guide。)

// resolvePickupDropoff 解析取还车 POI(MCP: search_locations → resolve_poi)。
// 铁律:keyword 必须来自 state.Slot.PickupText(LLM 抽的地点或用户答复的取车地点)。
// 严禁再回退到 in.UserInput —— 用户原话里几乎都是需求/闲聊,搜出来会是"一个人的酒馆"这种毒数据。
func (c *SearchCapability) resolvePickupDropoff(ctx context.Context, in CapabilityInput) (poiData, error) {
	var poi poiData
	keyword := in.State.Slot.PickupText
	if keyword == "" {
		return poi, fmt.Errorf("resolve_poi: empty pickup_text (guarded before call)")
	}

	locArgs, _ := json.Marshal(map[string]any{"keyword": keyword})
	locRes := in.Deps.Call(ctx, tools.ToolSearchLocations, string(locArgs))
	if locRes.IsError {
		return poi, fmt.Errorf("search_locations: %s", locRes.Debug)
	}
	data, ok := parseStdResp(locRes.Data)
	if !ok {
		return poi, fmt.Errorf("search_locations: bad resp")
	}
	var locs locationsData
	if err := json.Unmarshal(data, &locs); err != nil || len(locs.Locations) == 0 {
		return poi, fmt.Errorf("search_locations: no location")
	}
	loc := locs.Locations[0]

	poiArgs, _ := json.Marshal(map[string]any{"location_id": loc.LocationID})
	poiRes := in.Deps.Call(ctx, tools.ToolResolvePOI, string(poiArgs))
	if poiRes.IsError {
		return poi, fmt.Errorf("resolve_poi: %s", poiRes.Debug)
	}
	pdata, ok := parseStdResp(poiRes.Data)
	if !ok {
		return poi, fmt.Errorf("resolve_poi: bad resp")
	}
	// tyche 返回 data.poi.{...}, 必须走 envelope，不能直接解到 poi。
	var env poiEnvelope
	if err := json.Unmarshal(pdata, &env); err != nil {
		return poi, fmt.Errorf("resolve_poi: %w", err)
	}
	poi = env.POI
	poi.LocationID = loc.LocationID
	if poi.CityID == 0 {
		return poi, fmt.Errorf("resolve_poi: empty city_id, raw=%s", truncateForLog(string(pdata), 200))
	}

	applyPickupPOI(in.State, poi, true)
	return poi, nil
}

// truncateForLog 截断日志字符串,防炸屏。
func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// streamGuide 用 LLM 生成引导语并流式下发。
func (c *SearchCapability) streamGuide(ctx context.Context, in CapabilityInput, summary string) string {
	model, err := in.Factory.Get("search_guide")
	if err != nil {
		return fallbackGuide(summary)
	}
	userMsg := "真实搜到的车型摘要:\n" + summary
	ch, err := model.ChatStream(llm.WithStage(ctx, "capability:search_guide"), llm.ChatRequest{
		System:   prompt.SearchGuideSystem,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: userMsg}},
	})
	if err != nil {
		text := fallbackGuide(summary)
		if in.Emit != nil {
			in.Emit.Text(text)
		}
		return text
	}
	var b strings.Builder
	for chunk := range ch {
		if chunk.Err != nil {
			break
		}
		if chunk.Delta != "" {
			b.WriteString(chunk.Delta)
			if in.Emit != nil {
				in.Emit.Text(chunk.Delta)
			}
		}
	}
	if strings.TrimSpace(b.String()) == "" {
		text := fallbackGuide(summary)
		if in.Emit != nil {
			in.Emit.Text(text)
		}
		return text
	}
	return b.String()
}

func fallbackGuide(summary string) string {
	return "为你找到几辆合适的车,看看哪辆中意。以下单时为准。"
}

// summarizeQuotes 把报价压成给 LLM 看的摘要(含车名/价格,不含 ID)。
func summarizeQuotes(quotes []quoteItem) string {
	limit := 3
	if len(quotes) < limit {
		limit = len(quotes)
	}
	var b strings.Builder
	for i := 0; i < limit; i++ {
		q := quotes[i]
		fmt.Fprintf(&b, "%d. %s(%s,%d座,%s)日均%.0f元,总价%.0f元\n",
			i+1, q.CarName, q.BrandName, q.Seats, q.FuelType, q.DailyPrice, q.TotalPrice)
	}
	return b.String()
}

func filterExcludedQuotes(quotes []quoteItem, excludedRefs []string) []quoteItem {
	if len(quotes) == 0 || len(excludedRefs) == 0 {
		return quotes
	}
	excluded := make(map[string]struct{}, len(excludedRefs))
	for _, ref := range excludedRefs {
		excluded[ref] = struct{}{}
	}
	filtered := make([]quoteItem, 0, len(quotes))
	for _, q := range quotes {
		if _, ok := excluded[q.ReferenceID]; ok {
			continue
		}
		filtered = append(filtered, q)
	}
	if len(filtered) == 0 {
		return quotes
	}
	return filtered
}

func filterNegativeNeedQuotes(quotes []quoteItem, needs []types.UserNeed) []quoteItem {
	if len(quotes) == 0 || len(needs) == 0 {
		return quotes
	}
	filtered := make([]quoteItem, 0, len(quotes))
	for _, q := range quotes {
		if quoteMatchesNegativeNeed(q, needs) {
			continue
		}
		filtered = append(filtered, q)
	}
	if len(filtered) == 0 {
		return quotes
	}
	return filtered
}

func quoteMatchesNegativeNeed(q quoteItem, needs []types.UserNeed) bool {
	for _, n := range needs {
		if !n.Negative {
			continue
		}
		val := strings.TrimSpace(needValueString(n.Value))
		if val == "" {
			continue
		}
		switch n.Type {
		case "brand":
			if strings.Contains(q.BrandName, val) || strings.Contains(q.CarName, val) {
				return true
			}
		case "vehicle_model", "vehicle_series":
			if strings.Contains(q.CarName, val) {
				return true
			}
		case "energy_type":
			if strings.Contains(q.FuelType, val) {
				return true
			}
		}
	}
	return false
}

func quoteRefs(quotes []orchestration.QuoteRef) []string {
	refs := make([]string, 0, len(quotes))
	for _, q := range quotes {
		if q.ReferenceID != "" {
			refs = append(refs, q.ReferenceID)
		}
	}
	return refs
}

func applyPickupPOI(st *orchestration.ConversationState, poi poiData, mirrorDropoff bool) {
	if st == nil {
		return
	}
	st.Rental.PickupCityID = poi.CityID
	st.Rental.PickupName = poi.Name
	st.Rental.PickupPOI = rentalPOIFromData(poi)
	if mirrorDropoff {
		applyDropoffPOI(st, poi)
	}
}

func applyDropoffPOI(st *orchestration.ConversationState, poi poiData) {
	if st == nil {
		return
	}
	st.Rental.DropoffCityID = poi.CityID
	st.Rental.DropoffName = poi.Name
	st.Rental.DropoffPOI = rentalPOIFromData(poi)
}

func rentalPOIFromData(poi poiData) orchestration.RentalPOI {
	return orchestration.RentalPOI{
		LocationID: poi.LocationID,
		CityID:     poi.CityID,
		Name:       poi.Name,
		Latitude:   poi.Latitude,
		Longitude:  poi.Longitude,
	}
}

func attachGuidePOI(info *tyche.GuideRentalInfo, poi orchestration.RentalPOI) {
	if info == nil || poi.Latitude == 0 || poi.Longitude == 0 {
		return
	}
	info.POI = &tyche.GuideRentalInfoPOI{
		Latitude:  poi.Latitude,
		Longitude: poi.Longitude,
	}
}

func minQuotePrice(quotes []quoteItem) float64 {
	if len(quotes) == 0 {
		return 0
	}
	min := quotes[0].DailyPrice
	for _, q := range quotes[1:] {
		if q.DailyPrice < min {
			min = q.DailyPrice
		}
	}
	return min
}

func maxQuotePrice(quotes []quoteItem) float64 {
	if len(quotes) == 0 {
		return 0
	}
	max := quotes[0].DailyPrice
	for _, q := range quotes[1:] {
		if q.DailyPrice > max {
			max = q.DailyPrice
		}
	}
	return max
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
