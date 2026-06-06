package app

import (
	"reflect"
	"strings"
	"testing"
)

func TestDefaultFeatureResourceInventoryCoversDefaultOptionalFeatures(t *testing.T) {
	inventory := DefaultFeatureResourceInventory()
	byName := make(map[string]FeatureResourceInventoryItem, len(inventory))
	for _, item := range inventory {
		if item.FeatureName == "" {
			t.Fatal("expected inventory item feature name")
		}
		if item.Owner == "" {
			t.Fatalf("expected owner for inventory item %q", item.FeatureName)
		}
		if _, exists := byName[item.FeatureName]; exists {
			t.Fatalf("duplicate inventory item for %q", item.FeatureName)
		}
		byName[item.FeatureName] = item
	}

	for _, feature := range (&App{}).defaultFeatureList(DefaultFeatureFlags()) {
		name := feature.Name()
		if name == "lifecycle-noop" {
			continue
		}
		if _, ok := byName[name]; !ok {
			t.Fatalf("expected resource inventory for default feature %q", name)
		}
	}
}

func TestDefaultFeatureResourceInventoryDocumentsBroadAppFields(t *testing.T) {
	inventory := DefaultFeatureResourceInventory()
	fields := make(map[string]string)
	appType := reflect.TypeOf(App{})
	for _, item := range inventory {
		for _, field := range item.AppFields {
			if previous, exists := fields[field]; exists {
				t.Fatalf("app field %q is assigned to both %q and %q", field, previous, item.FeatureName)
			}
			if _, exists := appType.FieldByName(field); !exists {
				t.Fatalf("inventory field %q for feature %q is not present on App", field, item.FeatureName)
			}
			fields[field] = item.FeatureName
		}
	}

	for _, field := range []string{
		"TextResources",
		"GizmoResources",
		"SkyboxResources",
		"WaterResources",
		"AccumulationResources",
		"CAVolumeResources",
		"AnalyticMediumResources",
		"AstronomicalResources",
		"PlanetBodyResources",
		"FarPlanetRingResources",
		"DebrisMidfieldResources",
		"ParticleResources",
		"SpriteResources",
	} {
		if owner, ok := fields[field]; !ok {
			t.Fatalf("expected inventory owner for broad app field %q", field)
		} else if owner == "core" {
			t.Fatalf("expected feature-owned field %q to stay out of core inventory", field)
		}
	}

	if skybox := inventoryItem(inventory, "skybox"); !stringInSlice(skybox.AppFields, "SkyboxResources") || len(skybox.BufferManagerState) == 0 {
		t.Fatalf("expected skybox to document input holder and buffer-manager-owned resources, got %+v", skybox)
	}
}

func TestDefaultFeatureResourceInventoryUsesResourceHoldersForOptionalAppState(t *testing.T) {
	for _, item := range DefaultFeatureResourceInventory() {
		if item.Owner == FeatureResourceOwnerCore {
			continue
		}
		for _, field := range item.AppFields {
			if !strings.HasSuffix(field, "Resources") {
				t.Fatalf("expected non-core app field %q for feature %q to be a resource holder", field, item.FeatureName)
			}
		}
	}
}

func inventoryItem(inventory []FeatureResourceInventoryItem, name string) FeatureResourceInventoryItem {
	for _, item := range inventory {
		if item.FeatureName == name {
			return item
		}
	}
	return FeatureResourceInventoryItem{}
}

func stringInSlice(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
