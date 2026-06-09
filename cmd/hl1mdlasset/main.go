package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/importers/hl1"
)

func main() {
	var mdlPath string
	var outPath string
	var name string
	var sourceRef string
	var voxelResolution float32
	var metadataOnly bool
	flag.StringVar(&mdlPath, "mdl", "", "source GoldSrc MDL path")
	flag.StringVar(&outPath, "out", "", "output .gkasset path")
	flag.StringVar(&name, "name", "", "generated asset name")
	flag.StringVar(&sourceRef, "source-ref", "", "source reference stored in asset tags")
	flag.Var((*float32Flag)(&voxelResolution), "voxel-resolution", "voxel resolution for generated voxel asset")
	flag.BoolVar(&metadataOnly, "metadata-only", false, "print parsed MDL metadata without voxelizing or writing an asset")
	flag.Parse()

	if mdlPath == "" {
		fatalf("-mdl is required")
	}
	if outPath == "" && !metadataOnly {
		fatalf("-out is required")
	}
	if voxelResolution <= 0 {
		voxelResolution = hl1.DefaultNPCVoxelResolution
	}
	if metadataOnly {
		data, err := os.ReadFile(mdlPath)
		if err != nil {
			fatalf("read mdl: %v", err)
		}
		info, err := hl1.ParseMDLInfo(data)
		if err != nil {
			fatalf("parse mdl info: %v", err)
		}
		fmt.Printf("MDL: %s\n", mdlPath)
		fmt.Printf("name: %s\n", info.Name)
		fmt.Printf("bones: %d\n", len(info.Bones))
		fmt.Printf("sequences: %d\n", len(info.Sequences))
		for i, seq := range info.Sequences {
			fmt.Printf("sequence[%d]: name=%s fps=%g frames=%d blends=%d seqgroup=%d anim_index=%d decoded_bones=%d\n", i, seq.Name, seq.FPS, seq.FrameCount, seq.NumBlends, seq.SeqGroup, seq.AnimIndex, len(seq.BoneAnimations))
		}
		fmt.Printf("body_parts: %d\n", len(info.BodyParts))
		return
	}
	geometry, err := hl1.LoadMDLGeometry(mdlPath)
	if err != nil {
		fatalf("load mdl: %v", err)
	}
	asset, voxelCount, err := hl1.BuildMDLVoxelAsset(geometry, hl1.MDLVoxelAssetOptions{
		Name:            name,
		SourceRef:       sourceRef,
		VoxelResolution: voxelResolution,
	})
	if err != nil {
		fatalf("build asset: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0755); err != nil {
		fatalf("create output dir: %v", err)
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{DocumentPath: outPath}); validation.HasErrors() {
		fatalf("generated asset validation failed: %s", validation.Error())
	}
	if err := content.SaveAsset(outPath, asset); err != nil {
		fatalf("save asset: %v", err)
	}
	fmt.Printf("MDL asset written: %s\n", outPath)
	fmt.Printf("voxels: %d\n", voxelCount)
	fmt.Printf("parts: %d\n", len(asset.Parts))
	if asset.Skeleton != nil {
		fmt.Printf("bones: %d\n", len(asset.Skeleton.Bones))
	} else {
		fmt.Printf("bones: 0\n")
	}
	fmt.Printf("clips: %d\n", len(asset.AnimationClips))
}

type float32Flag float32

func (f *float32Flag) Set(value string) error {
	parsed, err := strconv.ParseFloat(value, 32)
	if err != nil {
		return err
	}
	*f = float32Flag(float32(parsed))
	return nil
}

func (f *float32Flag) String() string {
	return fmt.Sprintf("%g", float32(*f))
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "hl1mdlasset: "+format+"\n", args...)
	os.Exit(1)
}
