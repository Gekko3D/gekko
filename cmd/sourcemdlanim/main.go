// sourcemdlanim bakes selected Source MDL v48 animation clips into an
// existing authored rig. It does not import Source meshes or materials.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/importers/source"
)

func main() {
	var mdlPath, assetPath, outPath, clips, prefix string
	var lockRoot bool
	flag.StringVar(&mdlPath, "mdl", "", "Source MDL v48 animation source")
	flag.StringVar(&assetPath, "asset", "", "target .gkasset rig")
	flag.StringVar(&outPath, "out", "", "output .gkanim path")
	flag.StringVar(&clips, "clips", "", "comma-separated Source sequence labels")
	flag.StringVar(&prefix, "prefix", "source", "generated clip ID prefix")
	flag.BoolVar(&lockRoot, "lock-root-motion", true, "preserve controller ownership of root translation")
	flag.Parse()
	if mdlPath == "" || assetPath == "" || outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: sourcemdlanim -mdl donor.mdl -asset target.gkasset -out animations.gkanim -clips a_climbmount,pipe_jump")
		os.Exit(2)
	}
	asset, err := content.LoadAsset(assetPath)
	if err != nil {
		fatal(err)
	}
	sequenceNames := splitNames(clips)
	animationSource, err := source.LoadMDLAnimations(mdlPath, sequenceNames)
	if err != nil {
		fatal(err)
	}
	clipsOut, ids, err := source.BakeAnimations(asset, animationSource, source.BakeOptions{SequenceNames: sequenceNames, ClipPrefix: prefix, LockRootMotion: lockRoot})
	if err != nil {
		fatal(err)
	}
	rig, clipsOut, err := content.BuildRigAnimationDocuments(asset, clipsOut)
	if err != nil {
		fatal(err)
	}
	rig.ID, rig.Name = asset.ID+".rig", asset.Name+" rig"
	rigPath := strings.TrimSuffix(outPath, filepath.Ext(outPath)) + ".gkrig"
	set := &content.AnimationSetDef{ID: asset.ID + "." + strings.TrimSpace(prefix), SchemaVersion: content.CurrentAnimationSetSchemaVersion, Name: asset.Name + " animations", Clips: clipsOut}
	set.RigPath, err = filepath.Rel(filepath.Dir(outPath), rigPath)
	if err != nil {
		fatal(err)
	}
	set.RigPath = filepath.ToSlash(set.RigPath)
	ref, err := filepath.Rel(filepath.Dir(assetPath), outPath)
	if err != nil {
		fatal(err)
	}
	ref = filepath.ToSlash(ref)
	if !slices.Contains(asset.AnimationSetPaths, ref) {
		asset.AnimationSetPaths = append(asset.AnimationSetPaths, ref)
	}
	if asset.DefaultAnimationClipID == "" {
		asset.DefaultAnimationClipID = ids[0]
	}
	var staged []stagedOutput
	defer func() {
		for _, file := range staged {
			_ = os.Remove(file.temporary)
		}
	}()
	for _, output := range []struct {
		path string
		save func(string) error
	}{
		{rigPath, func(path string) error { return content.SaveAnimationRig(path, rig) }},
		{outPath, func(path string) error { return content.SaveAnimationSet(path, set) }},
		{assetPath, func(path string) error { return content.SaveAsset(path, asset) }},
	} {
		file, err := stageOutputFile(output.path, output.save)
		if err != nil {
			fatal(err)
		}
		staged = append(staged, file)
	}
	for _, file := range staged {
		if err := os.Rename(file.temporary, file.target); err != nil {
			fatal(fmt.Errorf("replace %s: %w", file.target, err))
		}
	}
	staged = nil
	for _, id := range ids {
		fmt.Println(id)
	}
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

func splitNames(value string) []string {
	var out []string
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
