package agent

import "strings"

type RequiredSlot struct {
	Name        string
	Description string
	AskWhen     string
}

var requiredSlots = []RequiredSlot{
	{Name: "seat_num", Description: "乘坐人数/座位数", AskWhen: "人数不明且场景无法推断时优先确认"},
	{Name: "vehicle_type", Description: "车型偏好,如 SUV/轿车/商务车/经济型", AskWhen: "用户未表达车型且场景无法推断时可问"},
	{Name: "price_preference", Description: "预算/价格敏感度", AskWhen: "价格敏感但预算不清时可问"},
}

func RenderRequiredSlots() string {
	var b strings.Builder
	b.WriteString("关键参考维度(required slots):\n")
	for _, s := range requiredSlots {
		b.WriteString("- ")
		b.WriteString(s.Name)
		b.WriteString(": ")
		b.WriteString(s.Description)
		b.WriteString("; ask_when=")
		b.WriteString(s.AskWhen)
		b.WriteString("\n")
	}
	b.WriteString("- sufficiency: >=0.6 可直接推荐,<0.6 只问一个关键维度。\n")
	return b.String()
}
