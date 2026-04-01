package content

type ProceduralPrimitiveSpec struct {
	Primitive string
	Params    []string
}

var proceduralPrimitiveSpecs = map[string]ProceduralPrimitiveSpec{
	"cube": {
		Primitive: "cube",
		Params:    []string{"sx", "sy", "sz"},
	},
	"sphere": {
		Primitive: "sphere",
		Params:    []string{"radius"},
	},
	"cone": {
		Primitive: "cone",
		Params:    []string{"radius", "height"},
	},
	"pyramid": {
		Primitive: "pyramid",
		Params:    []string{"size", "height"},
	},
	"cylinder": {
		Primitive: "cylinder",
		Params:    []string{"radius", "height"},
	},
	"capsule": {
		Primitive: "capsule",
		Params:    []string{"radius", "height"},
	},
	"ramp": {
		Primitive: "ramp",
		Params:    []string{"sx", "sy", "sz"},
	},
}

func ProceduralPrimitiveSpecFor(primitive string) (ProceduralPrimitiveSpec, bool) {
	spec, ok := proceduralPrimitiveSpecs[primitive]
	return spec, ok
}
