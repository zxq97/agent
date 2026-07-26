package vehiclecatalog

import "testing"

func TestDefaultCatalogResolvesAliases(t *testing.T) {
	catalog := NewDefaultCatalog()
	resolution := catalog.Resolve(&ResolveInput{Name: "model-y", Type: EntityModel, BrandHint: "Tesla"})
	if (resolution.Status != ResolveExact && resolution.Status != ResolveAlias) || resolution.Entity == nil || resolution.Entity.CanonicalName != "Model Y" || resolution.Entity.BrandID != "brand:tesla" {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
}

func TestCatalogDoesNotGuessUnknownVehicle(t *testing.T) {
	resolution := NewDefaultCatalog().Resolve(&ResolveInput{Name: "不存在车型", Type: EntityModel})
	if resolution.Status != ResolveNotFound || resolution.Entity != nil {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
}

func TestCatalogRejectsConflictingKnownBrandHint(t *testing.T) {
	resolution := NewDefaultCatalog().Resolve(&ResolveInput{Name: "Model Y", Type: EntityModel, BrandHint: "丰田"})
	if resolution.Status != ResolveNotFound || resolution.Entity != nil {
		t.Fatalf("unexpected resolution: %#v", resolution)
	}
}

func TestCatalogExposesStableVersion(t *testing.T) {
	catalog := NewVersionedStaticCatalog("catalog-2026-07", nil)
	if catalog.Version() != "catalog-2026-07" {
		t.Fatalf("version=%q", catalog.Version())
	}
}

func TestCatalogKeepsModelSeriesBrandHierarchy(t *testing.T) {
	resolution := NewDefaultCatalog().Resolve(&ResolveInput{Name: "325li", Type: EntityModel, BrandHint: "BMW", SeriesHint: "3系"})
	if resolution.Entity == nil || resolution.Entity.ParentID != "series:bmw:3" || resolution.Entity.BrandID != "brand:bmw" {
		t.Fatalf("resolution=%#v", resolution)
	}
}
