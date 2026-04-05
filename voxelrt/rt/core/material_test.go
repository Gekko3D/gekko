package core

import "testing"

func TestNewGameplaySeeThroughMaterialDisablesOpticalGlassTerms(t *testing.T) {
	mat := NewGameplaySeeThroughMaterial([4]uint8{100, 120, 140, 255}, 0.6)

	if mat.Transparency != 0.6 {
		t.Fatalf("expected transparency 0.6, got %f", mat.Transparency)
	}
	if mat.Transmission != 0.0 {
		t.Fatalf("expected transmission disabled, got %f", mat.Transmission)
	}
	if mat.Density != 0.0 {
		t.Fatalf("expected density disabled, got %f", mat.Density)
	}
	if mat.Refraction != 0.0 {
		t.Fatalf("expected refraction disabled, got %f", mat.Refraction)
	}
}

func TestApplyGameplaySeeThroughPreservesOtherMaterialProperties(t *testing.T) {
	mat := Material{
		BaseColor:    [4]uint8{10, 20, 30, 255},
		Emissive:     [4]uint8{1, 2, 3, 255},
		Emission:     2.0,
		Transmission: 1.0,
		Density:      0.8,
		Refraction:   0.5,
		Roughness:    0.2,
		Metalness:    0.3,
		IOR:          1.7,
		Transparency: 0.1,
	}

	mat.ApplyGameplaySeeThrough(0.75)

	if mat.Transparency != 0.75 {
		t.Fatalf("expected transparency 0.75, got %f", mat.Transparency)
	}
	if mat.Transmission != 0.0 || mat.Density != 0.0 || mat.Refraction != 0.0 {
		t.Fatalf("expected gameplay see-through to disable optical terms, got transmission=%f density=%f refraction=%f", mat.Transmission, mat.Density, mat.Refraction)
	}
	if mat.Roughness != 0.2 || mat.Metalness != 0.3 || mat.IOR != 1.7 || mat.Emission != 2.0 {
		t.Fatal("expected non-transparency material properties to be preserved")
	}
}

func TestApplyGameplaySeeThroughClampsTransparency(t *testing.T) {
	mat := DefaultMaterial()
	mat.ApplyGameplaySeeThrough(2.0)
	if mat.Transparency != 1.0 {
		t.Fatalf("expected transparency to clamp to 1, got %f", mat.Transparency)
	}
	mat.ApplyGameplaySeeThrough(-1.0)
	if mat.Transparency != 0.0 {
		t.Fatalf("expected transparency to clamp to 0, got %f", mat.Transparency)
	}
}
