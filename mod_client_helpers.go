package gekko

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unsafe"

	"github.com/cogentcore/webgpu/wgpu"
)

func createVertexBufferLayout(vertexType any) wgpu.VertexBufferLayout {
	t := reflect.TypeOf(vertexType)
	if t.Kind() != reflect.Struct {
		panic("Vertex must be a struct")
	}

	var attributes []wgpu.VertexAttribute
	var offset uint64 = 0

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)

		if "layout" == field.Tag.Get("gekko") {
			format := parseFormat(field.Tag.Get("format"))
			location, err := strconv.Atoi(field.Tag.Get("location"))
			if nil != err {
				panic(err)
			}

			attributes = append(attributes, wgpu.VertexAttribute{
				ShaderLocation: uint32(location),
				Offset:         offset,
				Format:         format,
			})
		}

		// Add size of field to offset
		offset += uint64(field.Type.Size())
	}

	return wgpu.VertexBufferLayout{
		ArrayStride: offset,
		StepMode:    wgpu.VertexStepModeVertex,
		Attributes:  attributes,
	}
}

func parseFormat(name string) wgpu.VertexFormat {
	switch name {
	case "float2":
		return wgpu.VertexFormatFloat32x2
	case "float3":
		return wgpu.VertexFormatFloat32x3
	case "float4":
		return wgpu.VertexFormatFloat32x4
	default:
		panic("unsupported vertex layout format: " + name)
	}
}

func untypedSliceToWgpuBytes(src AnySlice) []byte {
	l := src.Len()
	if l == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(src.DataPointer()), l*src.ElementSize())
}

func wgpuWrapMode(mode string) wgpu.AddressMode {
	switch mode {
	case "wrap":
		return wgpu.AddressModeRepeat
	case "mirror":
		return wgpu.AddressModeMirrorRepeat
	case "clamp":
		return wgpu.AddressModeClampToEdge
	default:
		panic(fmt.Sprintf("Unknown wrap mode: %s", mode))
	}
}

func wgpuFilterMode(mode string) wgpu.FilterMode {
	switch mode {
	case "nearest":
		return wgpu.FilterModeNearest
	case "linear":
		return wgpu.FilterModeLinear
	default:
		panic(fmt.Sprintf("Unknown filter mode: %s", mode))
	}
}

func findTextureDescriptors(entityId EntityId, cmd *Commands, assets *AssetServer) map[AssetId]textureDescriptor {
	descriptors := map[AssetId]textureDescriptor{}
	assetIdType := reflect.TypeOf(AssetId(""))
	allComponents := cmd.GetAllComponents(entityId)
	for _, c := range allComponents {
		val := reflect.ValueOf(c)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}
		t := val.Type()
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if "texture" == field.Tag.Get("gekko") {
				if field.Type != assetIdType {
					panic("Texture field must be type of AssetId")
				}
				group, err := strconv.Atoi(field.Tag.Get("group"))
				if nil != err {
					panic(err)
				}
				binding, err := strconv.Atoi(field.Tag.Get("binding"))
				if nil != err {
					panic(err)
				}
				fieldVal := val.Field(i)
				assetId := AssetId(fieldVal.String())
				textureAsset := assets.textures[assetId]

				descriptors[assetId] = textureDescriptor{
					version:      textureAsset.version,
					group:        uint32(group),
					binding:      uint32(binding),
					textureAsset: &textureAsset,
				}
			}
		}
	}
	return descriptors
}

func tryFindSamplers(cmd *Commands, entityId EntityId) (res []struct {
	assetId  AssetId
	group    uint32
	binding  uint32
	filter   string
	wrapMode string
}) {
	for _, c := range cmd.GetAllComponents(entityId) {
		ok, assetId, group, binding, filter, wrapMode := tryParseSamplerTags(c)
		if !ok {
			continue
		}

		res = append(
			res,
			struct {
				assetId  AssetId
				group    uint32
				binding  uint32
				filter   string
				wrapMode string
			}{
				assetId:  assetId,
				group:    group,
				binding:  binding,
				filter:   filter,
				wrapMode: wrapMode,
			},
		)
	}
	return
}

func tryParseSamplerTags(comp any) (ok bool, assetId AssetId, group uint32, binding uint32, filter string, wrapMode string) {
	val := reflect.ValueOf(comp)
	if val.Kind() == reflect.Ptr {
		val = val.Elem()
	}

	t := val.Type()
	for i := 0; i < t.NumField(); i++ {
		fieldDecl := t.Field(i)
		if "sampler" == fieldDecl.Tag.Get("gekko") {
			parsed_group, err := strconv.Atoi(fieldDecl.Tag.Get("group"))
			if nil != err {
				panic(err)
			} else {
				group = uint32(parsed_group)
			}

			parsed_binding, err := strconv.Atoi(fieldDecl.Tag.Get("binding"))
			if nil != err {
				panic(err)
			} else {
				binding = uint32(parsed_binding)
			}

			filter = fieldDecl.Tag.Get("filter")
			if "" == filter {
				filter = "linear"
			} else {
				filter = strings.ToLower(filter)
			}

			wrapMode = fieldDecl.Tag.Get("mode")
			if "" == wrapMode {
				wrapMode = "wrap"
			} else {
				wrapMode = strings.ToLower(wrapMode)
			}

			field := val.Field(i)
			if field.Kind() == reflect.Ptr {
				if field.IsNil() {
					panic("nil ptr")
				}
				field = field.Elem()
			}

			assetId = field.Interface().(AssetId)
			ok = true
			return
		}
	}
	ok = false
	return
}

func findBufferDescriptors(entityId EntityId, cmd *Commands) map[bufferId]bufferDescriptor {
	descriptors := map[bufferId]bufferDescriptor{}
	allComponents := cmd.GetAllComponents(entityId)
	for _, c := range allComponents {
		val := reflect.ValueOf(c)
		if val.Kind() == reflect.Ptr {
			val = val.Elem()
		}

		t := val.Type()
		for i := 0; i < t.NumField(); i++ {
			fieldDecl := t.Field(i)
			if "uniforms" == fieldDecl.Tag.Get("gekko") {
				group, err := strconv.Atoi(fieldDecl.Tag.Get("group"))
				if nil != err {
					panic(err)
				}
				binding, err := strconv.Atoi(fieldDecl.Tag.Get("binding"))
				if nil != err {
					panic(err)
				}

				//TODO un-hardcode
				//bufferUsages := parseBufferUsages()
				field := val.Field(i)
				if field.Kind() == reflect.Ptr {
					if field.IsNil() {
						panic("nil ptr")
					}
					field = field.Elem()
				}

				buf := new(bytes.Buffer)
				readUniformsBytes(field, buf)

				id := bufferId{
					group:   uint32(group),
					binding: uint32(binding),
				}
				descriptors[id] = bufferDescriptor{
					group:   uint32(group),
					binding: uint32(binding),
					data:    buf.Bytes(),
				}
			}
		}
	}
	return descriptors
}

func readUniformsBytes(field reflect.Value, buf *bytes.Buffer) {
	switch field.Kind() {
	case reflect.Slice, reflect.Array:
		for i := 0; i < field.Len(); i++ {
			elem := field.Index(i)
			if elem.Kind() == reflect.Ptr {
				elem = elem.Elem()
			}
			if err := binary.Write(buf, binary.LittleEndian, elem.Interface()); err != nil {
				panic(fmt.Errorf("failed to write slice element: %w", err))
			}
		}

	case reflect.Struct:
		for i := 0; i < field.NumField(); i++ {
			readUniformsBytes(field.Field(i), buf)
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32,
		reflect.Int8, reflect.Int16, reflect.Int32,
		reflect.Float32:
		if err := binary.Write(buf, binary.LittleEndian, field.Interface()); err != nil {
			panic(fmt.Errorf("failed to write scalar field: %w", err))
		}

	default:
		panic(fmt.Errorf("unsupported uniform type: %v", field))
	}
}

func wgpuBytesPerPixel(format wgpu.TextureFormat) uint {
	switch format {
	// the below is 1 byte per pixel
	case wgpu.TextureFormatR8Unorm:
	case wgpu.TextureFormatR8Snorm:
	case wgpu.TextureFormatR8Uint:
		return 1
	case wgpu.TextureFormatR8Sint:
		return 1

		// the below is 2 bytes per pixel
	case wgpu.TextureFormatR16Uint:
	case wgpu.TextureFormatR16Sint:
	case wgpu.TextureFormatR16Float:
	case wgpu.TextureFormatRG8Unorm:
	case wgpu.TextureFormatRG8Snorm:
	case wgpu.TextureFormatRG8Uint:
	case wgpu.TextureFormatRG8Sint:
		return 2

		// the below is 4 bytes per pixel
	case wgpu.TextureFormatR32Float:
	case wgpu.TextureFormatR32Uint:
	case wgpu.TextureFormatR32Sint:
	case wgpu.TextureFormatRG16Uint:
	case wgpu.TextureFormatRG16Sint:
	case wgpu.TextureFormatRG16Float:
	case wgpu.TextureFormatRGBA8Unorm:
		return 4
	case wgpu.TextureFormatRGBA8UnormSrgb:
	case wgpu.TextureFormatRGBA8Snorm:
	case wgpu.TextureFormatRGBA8Uint:
		return 4
	case wgpu.TextureFormatRGBA8Sint:
	case wgpu.TextureFormatBGRA8Unorm:
	case wgpu.TextureFormatBGRA8UnormSrgb:
	case wgpu.TextureFormatRGB10A2Uint:
	case wgpu.TextureFormatRGB10A2Unorm:
	case wgpu.TextureFormatRG11B10Ufloat:
	case wgpu.TextureFormatRGB9E5Ufloat:
		return 4

		// the below is 8 bytes per pixel
	case wgpu.TextureFormatRG32Float:
	case wgpu.TextureFormatRG32Uint:
	case wgpu.TextureFormatRG32Sint:
	case wgpu.TextureFormatRGBA16Uint:
	case wgpu.TextureFormatRGBA16Sint:
	case wgpu.TextureFormatRGBA16Float:
		return 8

		// the below is 16 bytes per pixel
	case wgpu.TextureFormatRGBA32Float:
	case wgpu.TextureFormatRGBA32Uint:
	case wgpu.TextureFormatRGBA32Sint:
		return 16
	}
	panic("Add missing texture format")
}
