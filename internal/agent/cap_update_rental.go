package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/zxq97/rental-agent/internal/orchestration"
	"github.com/zxq97/rental-agent/internal/tools"
)

// UpdateRentalCapability 处理"改取/还车地点或时间"。
//
// 行为:
//  1. 有 pickup_text → MCP search_locations + resolve_poi → 重写 Rental.Pickup*
//  2. 有 dropoff_text → 同上 → 重写 Rental.Dropoff*
//  3. clear_dropoff=true → 还车恢复同点
//  4. 有 pickup_time/dropoff_time → 解析并写入
//  5. 任一字段改动 → ResetForRentalChange(清 context_id/quotes/lastSearch/menu/selectedRef)
//  6. 返回确认话术 + 提示重搜
type UpdateRentalCapability struct{}

func (c *UpdateRentalCapability) Name() string { return "update_rental" }

func (c *UpdateRentalCapability) Run(ctx context.Context, in CapabilityInput) (*CapabilityResult, error) {
	args := in.Decision.Args
	st := in.State
	logger := in.Deps.Logger

	pickupText, _ := args["pickup_text"].(string)
	dropoffText, _ := args["dropoff_text"].(string)
	pickupTimeStr, _ := args["pickup_time"].(string)
	dropoffTimeStr, _ := args["dropoff_time"].(string)
	clearDropoff, _ := args["clear_dropoff"].(bool)

	changed := false
	var msgs []string
	hadSearchContext := hasSearchContext(st)

	// 1. 改取车地点
	if pickupText = strings.TrimSpace(pickupText); pickupText != "" {
		logf(logger, "[update_rental] stage=capability:update_rental step=resolve_pickup text=%q", pickupText)
		st.Slot.PickupText = pickupText
		poi, err := resolveLocation(ctx, in, pickupText)
		if err != nil {
			logf(logger, "[update_rental] stage=capability:update_rental step=resolve_pickup status=error err=%v", err)
			return &CapabilityResult{
				Clarification: &Clarification{
					Question: fmt.Sprintf("「%s」我没找到,能换个更具体的地标或街道吗?", pickupText),
					Slot:     "pickup_location",
				},
			}, nil
		}
		applyPickupPOI(st, poi, st.Rental.DropoffCityID == 0 || clearDropoff)
		// 无独立 dropoff → 默认同点还车
		msgs = append(msgs, "取车点改为「"+poi.Name+"」")
		changed = true
		logf(logger, "[update_rental] stage=capability:update_rental step=resolve_pickup status=ok city=%d name=%s", poi.CityID, poi.Name)
	}

	// 2. 改还车地点(异地还车)
	if dropoffText = strings.TrimSpace(dropoffText); dropoffText != "" && !clearDropoff {
		logf(logger, "[update_rental] stage=capability:update_rental step=resolve_dropoff text=%q", dropoffText)
		poi, err := resolveLocation(ctx, in, dropoffText)
		if err != nil {
			logf(logger, "[update_rental] stage=capability:update_rental step=resolve_dropoff status=error err=%v", err)
			return &CapabilityResult{
				Clarification: &Clarification{
					Question: fmt.Sprintf("「%s」还车点我没查到,能再确认一下吗?", dropoffText),
					Slot:     "dropoff_location",
				},
			}, nil
		}
		applyDropoffPOI(st, poi)
		msgs = append(msgs, "还车点改为「"+poi.Name+"」")
		changed = true
		logf(logger, "[update_rental] stage=capability:update_rental step=resolve_dropoff status=ok city=%d name=%s", poi.CityID, poi.Name)
	}

	// 3. 恢复同点还车
	if clearDropoff && st.Rental.PickupCityID > 0 {
		st.Rental.DropoffCityID = st.Rental.PickupCityID
		st.Rental.DropoffName = st.Rental.PickupName
		st.Rental.DropoffPOI = st.Rental.PickupPOI
		msgs = append(msgs, "还车恢复同取车点")
		changed = true
	}

	// 4. 改时间
	if pickupTimeStr = strings.TrimSpace(pickupTimeStr); pickupTimeStr != "" {
		if t, err := parseRentalTime(pickupTimeStr); err == nil {
			st.Rental.PickupTime = t
			msgs = append(msgs, "取车时间改为 "+t.Format("01-02 15:04"))
			changed = true
		}
	}
	if dropoffTimeStr = strings.TrimSpace(dropoffTimeStr); dropoffTimeStr != "" {
		if t, err := parseRentalTime(dropoffTimeStr); err == nil {
			st.Rental.DropoffTime = t
			msgs = append(msgs, "还车时间改为 "+t.Format("01-02 15:04"))
			changed = true
		}
	}

	if !changed {
		msg := "你想改什么呢?可以告诉我新的取车地点、还车地点或时间。"
		emitText(in, msg)
		return &CapabilityResult{Text: msg}, nil
	}

	// 5. 清除绑定在旧参数上的数据
	st.ResetForRentalChange()

	// 6. state 汇总
	if logger != nil {
		fmt.Fprintln(logger, orchestration.SummarizeForLog(st, "rental_updated"))
	}

	summary := "好的," + strings.Join(msgs, "、") + "。要不要重新看看车?"
	if hadSearchContext && rentalSearchReady(st) {
		prefix := "好的," + strings.Join(msgs, "、") + "。我继续按刚才的条件帮你搜。\n"
		emitText(in, prefix)
		searchRes, err := (&SearchCapability{}).Run(ctx, CapabilityInput{
			State:     st,
			UserInput: in.UserInput,
			Decision:  &Decision{Tool: ToolSearchVehicles, SearchMode: SearchModeRefine},
			Deps:      in.Deps,
			Factory:   in.Factory,
			Emit:      in.Emit,
		})
		if err != nil {
			return &CapabilityResult{Text: prefix + "但这次搜索失败了,可以稍后再试。"}, nil
		}
		if searchRes != nil {
			searchRes.Text = prefix + searchRes.Text
			return searchRes, nil
		}
		return &CapabilityResult{Text: prefix}, nil
	}
	emitText(in, summary)
	return &CapabilityResult{Text: summary}, nil
}

// resolveLocation 通用地点解析(MCP search_locations → resolve_poi)。
func resolveLocation(ctx context.Context, in CapabilityInput, keyword string) (poiData, error) {
	var poi poiData
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
		return poi, fmt.Errorf("search_locations: no location for %q", keyword)
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
	var env poiEnvelope
	if err := json.Unmarshal(pdata, &env); err != nil {
		return poi, fmt.Errorf("resolve_poi: %w", err)
	}
	poi = env.POI
	poi.LocationID = loc.LocationID
	if poi.CityID == 0 {
		return poi, fmt.Errorf("resolve_poi: empty city_id for %q", keyword)
	}
	return poi, nil
}

// parseRentalTime 解析用户给出的时间字符串。支持 "YYYY-MM-DD HH:MM:SS" 和 "YYYY-MM-DD HH:MM"。
func parseRentalTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	layouts := []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-1-2 15:04:05", "2006-1-2 15:04"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, s, time.Local); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time: %q", s)
}

func hasSearchContext(st *orchestration.ConversationState) bool {
	if st == nil {
		return false
	}
	if st.LastSearch != nil || len(st.LastQuotes) > 0 {
		return true
	}
	c := st.Constraints
	return len(c.Hard)+len(c.Soft)+len(c.Negative) > 0
}
