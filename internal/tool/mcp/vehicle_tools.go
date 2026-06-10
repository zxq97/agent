package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// --- 车辆语义化 Tool ---
// 将原始 MCP 工具封装为对 VehicleAgent 更友好的语义化接口
// 每个语义化 Tool 内部调用 MCPClient.CallTool，但对参数和输出做格式化处理

// SearchLocationsInput search_pickup_locations 工具输入
type SearchLocationsInput struct {
	City    string `json:"city" jsonschema:"description=城市名称，如北京、上海" jsonschema_description:"城市名称"`
	Keyword string `json:"keyword" jsonschema:"description=搜索关键词，如望京、国贸" jsonschema_description:"搜索关键词"`
}

// SearchLocationsOutput search_pickup_locations 工具输出（格式化后）
type SearchLocationsOutput struct {
	Locations []LocationItem `json:"locations"`
}

// LocationItem 地点信息
type LocationItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// POIInfo 经纬度坐标
type POIInfo struct {
	Latitude  float64 `json:"latitude" jsonschema:"description=纬度" jsonschema_description:"纬度"`
	Longitude float64 `json:"longitude" jsonschema:"description=经度" jsonschema_description:"经度"`
}

// RentalInfoInput 取车/还车信息（对应 MCP 的 pickup_rental_info / dropoff_rental_info）
type RentalInfoInput struct {
	CityID       int     `json:"city_id" jsonschema:"description=城市ID，来自 resolve_location 返回的 poi.city_id" jsonschema_description:"城市ID"`
	LocationName string  `json:"location_name" jsonschema:"description=门店名称，来自 resolve_location 返回的 poi.name" jsonschema_description:"门店名称"`
	LocationCode string  `json:"location_code" jsonschema:"description=门店编码，可选" jsonschema_description:"门店编码"`
	DateTime     string  `json:"date_time" jsonschema:"description=时间，格式 YYYY-MM-DD HH:mm:ss，取车默认14:00:00" jsonschema_description:"取还车时间"`
	Poi          POIInfo `json:"poi" jsonschema:"description=经纬度坐标，来自 resolve_location 返回" jsonschema_description:"经纬度坐标"`
}

// SearchQuotesInput search_vehicle_quotes 工具输入
type SearchQuotesInput struct {
	PickupRentalInfo  RentalInfoInput `json:"pickup_rental_info" jsonschema:"description=取车信息（城市ID、门店名称、时间、经纬度）" jsonschema_description:"取车信息"`
	DropoffRentalInfo RentalInfoInput `json:"dropoff_rental_info" jsonschema:"description=还车信息（城市ID、门店名称、时间、经纬度），异店还车时必填" jsonschema_description:"还车信息"`
}

// SearchQuotesOutput search_vehicle_quotes 工具输出（格式化后）
type SearchQuotesOutput struct {
	Quotes []QuoteItem `json:"quotes"`
}

// QuoteItem 报价信息
type QuoteItem struct {
	CarModel   string `json:"car_model"`
	SeatCount  int    `json:"seat_count"`
	DailyPrice string `json:"daily_price"`
	TotalPrice string `json:"total_price"`
	Tags       string `json:"tags"`
}

// ResolveLocationInput resolve_location 工具输入
type ResolveLocationInput struct {
	LocationID string `json:"location_id" jsonschema:"description=地点ID" jsonschema_description:"地点ID"`
}

// ResolveLocationOutput resolve_location 工具输出（格式化后）
type ResolveLocationOutput struct {
	StoreName    string `json:"store_name"`
	Address      string `json:"address"`
	BusinessHour string `json:"business_hour"`
}

// NewVehicleTools 创建所有车辆语义化 Tool
func NewVehicleTools(client *MCPClient) ([]tool.BaseTool, error) {
	var tools []tool.BaseTool

	searchLoc, err := NewSearchPickupLocationsTool(client)
	if err != nil {
		return nil, fmt.Errorf("创建 search_pickup_locations 工具失败: %w", err)
	}
	tools = append(tools, searchLoc)

	searchQuotes, err := NewSearchVehicleQuotesTool(client)
	if err != nil {
		return nil, fmt.Errorf("创建 search_vehicle_quotes 工具失败: %w", err)
	}
	tools = append(tools, searchQuotes)

	resolveLoc, err := NewResolveLocationTool(client)
	if err != nil {
		return nil, fmt.Errorf("创建 resolve_location 工具失败: %w", err)
	}
	tools = append(tools, resolveLoc)

	return tools, nil
}

// NewSearchPickupLocationsTool 创建搜索取车点 Tool
func NewSearchPickupLocationsTool(client *MCPClient) (tool.InvokableTool, error) {
	return utils.InferTool[SearchLocationsInput, SearchLocationsOutput](
		"search_pickup_locations",
		"搜索取车地点。输入城市名和关键词，返回匹配的取车地点列表（含ID和名称）。",
		func(ctx context.Context, input SearchLocationsInput) (SearchLocationsOutput, error) {
			args := map[string]any{
				"city":    input.City,
				"keyword": input.Keyword,
			}
			result, err := client.CallTool(ctx, "rental_search_locations", args)
			if err != nil {
				return SearchLocationsOutput{}, fmt.Errorf("搜索取车地点失败: %w", err)
			}

			output := SearchLocationsOutput{}
			text := extractText(result.Content)
			if text != "" {
				// 尝试解析 MCP 返回的 JSON
				_ = json.Unmarshal([]byte(text), &output.Locations)
				if len(output.Locations) == 0 {
					// 如果解析失败，把原始文本作为单条记录
					output.Locations = []LocationItem{{Name: text}}
				}
			}
			return output, nil
		},
	)
}

// NewSearchVehicleQuotesTool 创建搜索车型报价 Tool
func NewSearchVehicleQuotesTool(client *MCPClient) (tool.InvokableTool, error) {
	return utils.InferTool[SearchQuotesInput, SearchQuotesOutput](
		"search_vehicle_quotes",
		"搜索可用车型及报价。需要提供取车信息（城市ID、门店名称、时间、经纬度）和还车信息，返回车型列表（含名称、座位数、日租金、总价、标签）。",
		func(ctx context.Context, input SearchQuotesInput) (SearchQuotesOutput, error) {
			pickupInfo := map[string]any{
				"city_id":       input.PickupRentalInfo.CityID,
				"location_name": input.PickupRentalInfo.LocationName,
				"date_time":     input.PickupRentalInfo.DateTime,
				"poi": map[string]any{
					"latitude":  input.PickupRentalInfo.Poi.Latitude,
					"longitude": input.PickupRentalInfo.Poi.Longitude,
				},
			}
			if input.PickupRentalInfo.LocationCode != "" {
				pickupInfo["location_code"] = input.PickupRentalInfo.LocationCode
			}

			dropoffInfo := map[string]any{
				"city_id":       input.DropoffRentalInfo.CityID,
				"location_name": input.DropoffRentalInfo.LocationName,
				"date_time":     input.DropoffRentalInfo.DateTime,
				"poi": map[string]any{
					"latitude":  input.DropoffRentalInfo.Poi.Latitude,
					"longitude": input.DropoffRentalInfo.Poi.Longitude,
				},
			}
			if input.DropoffRentalInfo.LocationCode != "" {
				dropoffInfo["location_code"] = input.DropoffRentalInfo.LocationCode
			}

			args := map[string]any{
				"pickup_rental_info":  pickupInfo,
				"dropoff_rental_info": dropoffInfo,
			}

			result, err := client.CallTool(ctx, "rental_search_quotes", args)
			if err != nil {
				return SearchQuotesOutput{}, fmt.Errorf("搜索车型报价失败: %w", err)
			}

			output := SearchQuotesOutput{}
			text := extractText(result.Content)
			if text != "" {
				_ = json.Unmarshal([]byte(text), &output.Quotes)
				if len(output.Quotes) == 0 {
					output.Quotes = []QuoteItem{{CarModel: text}}
				}
			}
			return output, nil
		},
	)
}

// NewResolveLocationTool 创建解析地点 Tool
func NewResolveLocationTool(client *MCPClient) (tool.InvokableTool, error) {
	return utils.InferTool[ResolveLocationInput, ResolveLocationOutput](
		"resolve_location",
		"根据地点ID解析出具体门店信息，包括门店名称、地址、营业时间。",
		func(ctx context.Context, input ResolveLocationInput) (ResolveLocationOutput, error) {
			args := map[string]any{
				"location_id": input.LocationID,
			}

			result, err := client.CallTool(ctx, "rental_resolve_poi", args)
			if err != nil {
				return ResolveLocationOutput{}, fmt.Errorf("解析地点失败: %w", err)
			}

			output := ResolveLocationOutput{}
			text := extractText(result.Content)
			if text != "" {
				_ = json.Unmarshal([]byte(text), &output)
				if output.StoreName == "" {
					output.StoreName = text
				}
			}
			return output, nil
		},
	)
}

// formatQuoteOutput 格式化报价输出为可读文本
func formatQuoteOutput(quotes []QuoteItem) string {
	if len(quotes) == 0 {
		return "未找到符合条件的车型。"
	}

	var sb strings.Builder
	for i, q := range quotes {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("- %s | %d座 | 日租:%s | 总价:%s", q.CarModel, q.SeatCount, q.DailyPrice, q.TotalPrice))
		if q.Tags != "" {
			sb.WriteString(fmt.Sprintf(" | %s", q.Tags))
		}
	}
	return sb.String()
}
