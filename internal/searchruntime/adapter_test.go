package searchruntime

import (
	"reflect"
	"testing"

	"github.com/zxq97/agent/api/guide"
)

func TestGuideAdaptersRoundTripPersistedRuntimeFields(t *testing.T) {
	menus := []guide.MenuGroup{{
		Name: "车型", GroupCode: "vehicle", GroupType: "filter",
		GroupItems: []guide.GroupItem{{Items: []guide.Item{{Name: "SUV", ItemCode: "filter/vehcle_choice/suv"}}}},
	}}
	quotes := []guide.VehRate{{
		SupplierCode: "supplier", SupplierDisplayName: "供应商",
		Vehicle: &guide.Vehicle{
			VehicleName: "示例车", VehicleCode: "vehicle-1", BrandName: "示例",
			GroupName: "SUV", Seats: 5, FuelType: 2, TransmissionType: 1,
		},
		DailyDeductionAmount: 99,
		TotalCharge: &guide.TotalCharge{
			TotalAmount: 300, DeductionAmount: 20, DeductionAmountInt: 20,
		},
		ReferenceInfo:   &guide.RefInfo{ReferenceID: "reference-1"},
		FreeDepositType: 1,
	}}
	if got := MenusToGuide(MenusFromGuide(menus)); !reflect.DeepEqual(got, menus) {
		t.Fatalf("menu round trip changed data: got=%#v want=%#v", got, menus)
	}
	if got := QuotesToGuide(QuotesFromGuide(quotes)); !reflect.DeepEqual(got, quotes) {
		t.Fatalf("quote round trip changed data: got=%#v want=%#v", got, quotes)
	}
}
