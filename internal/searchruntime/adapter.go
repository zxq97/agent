package searchruntime

import "github.com/zxq97/agent/api/guide"

func MenusFromGuide(values []guide.MenuGroup) []MenuGroup {
	result := make([]MenuGroup, len(values))
	for groupIndex := range values {
		result[groupIndex] = MenuGroup{
			Name:       values[groupIndex].Name,
			GroupCode:  values[groupIndex].GroupCode,
			GroupType:  values[groupIndex].GroupType,
			GroupItems: make([]GroupItem, len(values[groupIndex].GroupItems)),
		}
		for itemIndex := range values[groupIndex].GroupItems {
			items := values[groupIndex].GroupItems[itemIndex].Items
			result[groupIndex].GroupItems[itemIndex].Items = make([]MenuItem, len(items))
			for index := range items {
				result[groupIndex].GroupItems[itemIndex].Items[index] = MenuItem{Name: items[index].Name, Code: items[index].ItemCode}
			}
		}
	}
	return result
}

func MenusToGuide(values []MenuGroup) []guide.MenuGroup {
	result := make([]guide.MenuGroup, len(values))
	for groupIndex := range values {
		result[groupIndex] = guide.MenuGroup{
			Name:       values[groupIndex].Name,
			GroupCode:  values[groupIndex].GroupCode,
			GroupType:  values[groupIndex].GroupType,
			GroupItems: make([]guide.GroupItem, len(values[groupIndex].GroupItems)),
		}
		for itemIndex := range values[groupIndex].GroupItems {
			items := values[groupIndex].GroupItems[itemIndex].Items
			result[groupIndex].GroupItems[itemIndex].Items = make([]guide.Item, len(items))
			for index := range items {
				result[groupIndex].GroupItems[itemIndex].Items[index] = guide.Item{Name: items[index].Name, ItemCode: items[index].Code}
			}
		}
	}
	return result
}

func QuotesFromGuide(values []guide.VehRate) []Quote {
	result := make([]Quote, len(values))
	for index := range values {
		result[index] = Quote{
			SupplierCode:        values[index].SupplierCode,
			SupplierDisplayName: values[index].SupplierDisplayName,
			DailyDeduction:      values[index].DailyDeductionAmount,
			FreeDepositType:     values[index].FreeDepositType,
		}
		if values[index].Vehicle != nil {
			result[index].Vehicle = &Vehicle{
				Name:             values[index].Vehicle.VehicleName,
				Code:             values[index].Vehicle.VehicleCode,
				BrandName:        values[index].Vehicle.BrandName,
				GroupName:        values[index].Vehicle.GroupName,
				Seats:            values[index].Vehicle.Seats,
				FuelType:         values[index].Vehicle.FuelType,
				TransmissionType: values[index].Vehicle.TransmissionType,
			}
		}
		if values[index].TotalCharge != nil {
			result[index].TotalCharge = &Charge{
				TotalAmount:        values[index].TotalCharge.TotalAmount,
				DeductionAmount:    values[index].TotalCharge.DeductionAmount,
				DeductionAmountInt: values[index].TotalCharge.DeductionAmountInt,
			}
		}
		if values[index].ReferenceInfo != nil {
			result[index].Reference = &Reference{ID: values[index].ReferenceInfo.ReferenceID}
		}
	}
	return result
}

func QuotesToGuide(values []Quote) []guide.VehRate {
	result := make([]guide.VehRate, len(values))
	for index := range values {
		result[index] = guide.VehRate{
			SupplierCode:         values[index].SupplierCode,
			SupplierDisplayName:  values[index].SupplierDisplayName,
			DailyDeductionAmount: values[index].DailyDeduction,
			FreeDepositType:      values[index].FreeDepositType,
		}
		if values[index].Vehicle != nil {
			result[index].Vehicle = &guide.Vehicle{
				VehicleName:      values[index].Vehicle.Name,
				VehicleCode:      values[index].Vehicle.Code,
				BrandName:        values[index].Vehicle.BrandName,
				GroupName:        values[index].Vehicle.GroupName,
				Seats:            values[index].Vehicle.Seats,
				FuelType:         values[index].Vehicle.FuelType,
				TransmissionType: values[index].Vehicle.TransmissionType,
			}
		}
		if values[index].TotalCharge != nil {
			result[index].TotalCharge = &guide.TotalCharge{
				TotalAmount:        values[index].TotalCharge.TotalAmount,
				DeductionAmount:    values[index].TotalCharge.DeductionAmount,
				DeductionAmountInt: values[index].TotalCharge.DeductionAmountInt,
			}
		}
		if values[index].Reference != nil {
			result[index].ReferenceInfo = &guide.RefInfo{ReferenceID: values[index].Reference.ID}
		}
	}
	return result
}

func CloneMenus(values []MenuGroup) []MenuGroup {
	return MenusFromGuide(MenusToGuide(values))
}

func CloneQuotes(values []Quote) []Quote {
	return QuotesFromGuide(QuotesToGuide(values))
}
