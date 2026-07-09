package tools

import "testing"

func TestIsAllowedTool(t *testing.T) {
	allowed := []string{
		ToolSearchLocations, ToolResolvePOI,
		ToolGetOrderDetails, ToolGetReservation, ToolGetDriverList,
	}
	for _, name := range allowed {
		if !IsAllowedTool(name) {
			t.Errorf("%s should be allowed", name)
		}
	}

	// 写操作 + 已下线的 rental_search_quotes 必须被拒
	denied := []string{
		"rental_create_order", "rental_pay", "rental_refund",
		"rental_modify_order", "random_tool", "",
		ToolSearchQuotes, // 报价改走 rental-guide,MCP 版本禁用
	}
	for _, name := range denied {
		if IsAllowedTool(name) {
			t.Errorf("%s must NOT be allowed (write op / unknown / deprecated)", name)
		}
	}
}
