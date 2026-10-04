package gekko

import (
	"reflect"
	"unsafe"
)

// Only immutable strings can alias inside a constructor-owned publication copy.
// Keep the generic estimator's exact backing-view and concrete-type identity.
type compiledPalettePublicationString struct {
	data *byte
	len  int
	typ  reflect.Type
}

type compiledPalettePublicationStrings map[compiledPalettePublicationString]struct{}

var compiledPalettePublicationStringType = reflect.TypeOf("")

func (strings compiledPalettePublicationStrings) charge(value string, typ reflect.Type) int64 {
	if len(value) == 0 {
		return 0
	}
	identity := compiledPalettePublicationString{data: unsafe.StringData(value), len: len(value), typ: typ}
	if _, exists := strings[identity]; exists {
		return 0
	}
	strings[identity] = struct{}{}
	return int64(len(value))
}

// compiledPalettePublicationCharge applies only to the independent copy created
// by prepareCompiledPaletteRegistration. That constructor allocates every mutable
// map, slice and UV pointer independently and gives each slice cap == len. We can
// count their storage directly without a general graph's mutable identity table.
// Immutable strings still use the generic estimator's exact alias rules.
func compiledPalettePublicationCharge(palette *VoxelPaletteAsset) int64 {
	bytes := int64(unsafe.Sizeof(palette))
	if palette == nil {
		return bytes
	}
	strings := make(compiledPalettePublicationStrings)
	stringCharge := func(value string) int64 {
		return strings.charge(value, compiledPalettePublicationStringType)
	}
	bytes = runtimeContentChargeSum(bytes, int64(unsafe.Sizeof(*palette)), stringCharge(palette.SourcePath))

	bytes = runtimeContentChargeSum(bytes, runtimeContentChargeProduct(int64(cap(palette.Materials)), int64(unsafe.Sizeof(VoxMaterial{}))))
	for _, material := range palette.Materials {
		bytes = runtimeContentChargeSum(bytes, runtimeContentChargeProduct(int64(len(material.Property)), int64(unsafe.Sizeof(""))+int64(unsafe.Sizeof(interface{}(nil)))))
		for key, value := range material.Property {
			bytes = runtimeContentChargeSum(bytes, stringCharge(key))
			if value == nil {
				continue
			}
			typ := reflect.TypeOf(value)
			bytes = runtimeContentChargeSum(bytes, int64(typ.Size()))
			if typ.Kind() == reflect.String {
				bytes = runtimeContentChargeSum(bytes, strings.charge(reflect.ValueOf(value).String(), typ))
			}
		}
	}

	bytes = runtimeContentChargeSum(bytes, runtimeContentChargeProduct(int64(len(palette.SurfaceMaterials)), int64(unsafe.Sizeof(uint8(0)))+int64(unsafe.Sizeof(VoxelSurfaceMaterial{}))))
	for _, surface := range palette.SurfaceMaterials {
		bytes = runtimeContentChargeSum(bytes, stringCharge(surface.Kind), runtimeContentChargeProduct(int64(cap(surface.Tags)), int64(unsafe.Sizeof(""))))
		for _, tag := range surface.Tags {
			bytes = runtimeContentChargeSum(bytes, stringCharge(tag))
		}
	}
	bytes = runtimeContentChargeSum(bytes, runtimeContentChargeProduct(int64(len(palette.MaterialFrameOverrides)), int64(unsafe.Sizeof(uint8(0)))+int64(unsafe.Sizeof(VoxelPaletteMaterialFrameOverride{}))))

	bytes = runtimeContentChargeSum(bytes, runtimeContentChargeProduct(int64(cap(palette.Animations)), int64(unsafe.Sizeof(VoxelPaletteAnimation{}))))
	for _, animation := range palette.Animations {
		bytes = runtimeContentChargeSum(bytes, stringCharge(animation.ID), stringCharge(animation.Kind), stringCharge(animation.Mode),
			runtimeContentChargeProduct(int64(cap(animation.PaletteIndices)), int64(unsafe.Sizeof(uint8(0)))),
			runtimeContentChargeProduct(int64(cap(animation.Tags)), int64(unsafe.Sizeof(""))),
			runtimeContentChargeProduct(int64(cap(animation.Frames)), int64(unsafe.Sizeof(VoxelPaletteAnimationFrame{}))))
		for _, tag := range animation.Tags {
			bytes = runtimeContentChargeSum(bytes, stringCharge(tag))
		}
		if animation.UVScroll != nil {
			bytes = runtimeContentChargeSum(bytes, int64(unsafe.Sizeof(*animation.UVScroll)))
		}
		for _, frame := range animation.Frames {
			bytes = runtimeContentChargeSum(bytes,
				runtimeContentChargeProduct(int64(cap(frame.Colors)), int64(unsafe.Sizeof([4]uint8{}))),
				runtimeContentChargeProduct(int64(cap(frame.EmissiveColors)), int64(unsafe.Sizeof([4]uint8{}))),
				runtimeContentChargeProduct(int64(cap(frame.Emission)), int64(unsafe.Sizeof(float32(0)))),
				runtimeContentChargeProduct(int64(cap(frame.Roughness)), int64(unsafe.Sizeof(float32(0)))),
				runtimeContentChargeProduct(int64(cap(frame.Transparency)), int64(unsafe.Sizeof(float32(0)))))
		}
	}
	return bytes
}
