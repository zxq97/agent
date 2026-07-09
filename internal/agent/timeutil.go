package agent

import "time"

const tycheTimeLayout = "2006-01-02 15:04:05"

// defaultPickupTime 默认取车时间:明天 14:00:00。
// P1 简化:用户未明确取还时间时用默认值;P7 起由 decide 层从自然语言换算。
func defaultPickupTime() string {
	t := time.Now().AddDate(0, 0, 1)
	t = time.Date(t.Year(), t.Month(), t.Day(), 14, 0, 0, 0, t.Location())
	return t.Format(tycheTimeLayout)
}

// defaultDropoffTime 默认还车时间:后天 12:00:00。
func defaultDropoffTime() string {
	t := time.Now().AddDate(0, 0, 2)
	t = time.Date(t.Year(), t.Month(), t.Day(), 12, 0, 0, 0, t.Location())
	return t.Format(tycheTimeLayout)
}
