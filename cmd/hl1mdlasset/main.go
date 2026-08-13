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
			fmt.Printf("sequence[%d]: name=%s fps=%g frames=%d blends=%d seqgroup=%d anim_index=%d decoded_bones=%d\n", i, seq.Name, seq.FPS, seq.FrameCount, seq.NumBlends, seq.SeqGroup, seq.AnimIndex, len(seq.BoneAnimations)+len(seq.BlendAnimations)*len(info.Bones))
		}
		fmt.Printf("body_parts: %d\n", len(info.BodyParts))
		return
	}
	geometry, err := hl1.LoadMDLGeometry(mdlPath)
	if err != nil {
		fatalf("load mdl: %v", err)
	}
	built, err := hl1.BuildMDLVoxelAssetDocuments(geometry, hl1.MDLVoxelAssetOptions{
		Name:                name,
		SourceRef:           sourceRef,
		VoxelResolution:     voxelResolution,
		VoxelizationProfile: hl1.MDLVoxelizationProfileForCategory(hl1.HL1VoxelResolutionCategoryNPC),
	})
	if err != nil {
		fatalf("build asset: %v", err)
	}
	asset, voxelCount := built.Asset, built.VoxelCount
	animations, err := hl1.BuildMDLAnimationDocumentsAtRoot(asset, built.Clips, outPath, filepath.Dir(filepath.Dir(outPath)))
	if err != nil {
		fatalf("build animation documents: %v", err)
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{DocumentPath: outPath}); validation.HasErrors() {
		fatalf("generated asset validation failed: %s", validation.Error())
	}
	var staged []stagedOutput
	defer func() {
		for _, file := range staged {
			_ = os.Remove(file.temporary)
		}
	}()
	if animations.Rig != nil {
		file, err := stageOutputFile(animations.RigPath, func(path string) error { return content.SaveAnimationRig(path, animations.Rig) })
		if err != nil {
			fatalf("stage rig: %v", err)
		}
		staged = append(staged, file)
	}
	if animations.Set != nil {
		file, err := stageOutputFile(animations.SetPath, func(path string) error { return content.SaveAnimationSet(path, animations.Set) })
		if err != nil {
			fatalf("stage animation: %v", err)
		}
		staged = append(staged, file)
	}
	file, err := stageOutputFile(outPath, func(path string) error { return content.SaveAsset(path, asset) })
	if err != nil {
		fatalf("stage asset: %v", err)
	}
	staged = append(staged, file)
	for _, file := range staged {
		if err := os.Rename(file.temporary, file.target); err != nil {
			fatalf("replace %s: %v", file.target, err)
		}
	}
	staged = nil
	fmt.Printf("MDL asset written: %s\n", outPath)
	fmt.Printf("voxels: %d\n", voxelCount)
	fmt.Printf("parts: %d\n", len(asset.Parts))
	if asset.Skeleton != nil {
		fmt.Printf("bones: %d\n", len(asset.Skeleton.Bones))
	} else {
		fmt.Printf("bones: 0\n")
	}
	fmt.Printf("clips: %d\n", len(built.Clips))
}

type stagedOutput struct{ temporary, target string }

func stageOutputFile(path string, save func(string) error) (stagedOutput, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return stagedOutput{}, err
	}
	file, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return stagedOutput{}, err
	}
	temporary := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(temporary)
		return stagedOutput{}, err
	}
	if err := save(temporary); err != nil {
		_ = os.Remove(temporary)
		return stagedOutput{}, err
	}
	return stagedOutput{temporary: temporary, target: path}, nil
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
