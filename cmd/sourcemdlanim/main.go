// sourcemdlanim bakes selected Source MDL v48 animation clips into an
// existing authored rig. It does not import Source meshes or materials.
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/gekko3d/gekko/content"
	"github.com/gekko3d/gekko/importers/source"
)

func main() {
	var mdlPath, assetPath, outPath, clips, prefix string
	var lockRoot bool
	flag.StringVar(&mdlPath, "mdl", "", "Source MDL v48 animation source")
	flag.StringVar(&assetPath, "asset", "", "target .gkasset rig")
	flag.StringVar(&outPath, "out", "", "output .gkasset path")
	flag.StringVar(&clips, "clips", "", "comma-separated Source sequence labels")
	flag.StringVar(&prefix, "prefix", "source", "generated clip ID prefix")
	flag.BoolVar(&lockRoot, "lock-root-motion", true, "preserve controller ownership of root translation")
	flag.Parse()
	if mdlPath == "" || assetPath == "" || outPath == "" {
		fmt.Fprintln(os.Stderr, "usage: sourcemdlanim -mdl donor.mdl -asset target.gkasset -out target.gkasset -clips a_climbmount,pipe_jump")
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
	ids, err := source.AppendAnimations(asset, animationSource, source.BakeOptions{SequenceNames: sequenceNames, ClipPrefix: prefix, LockRootMotion: lockRoot})
	if err != nil {
		fatal(err)
	}
	if validation := content.ValidateAsset(asset, content.AssetValidationOptions{DocumentPath: outPath}); validation.HasErrors() {
		fatal(fmt.Errorf("generated asset validation failed: %s", validation.Error()))
	}
	if err := content.SaveAsset(outPath, asset); err != nil {
		fatal(err)
	}
	for _, id := range ids {
		fmt.Println(id)
	}
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
