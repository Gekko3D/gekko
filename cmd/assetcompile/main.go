package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gekko3d/gekko"
)

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("assetcompile", flag.ContinueOnError)
	flags.SetOutput(stderr)
	input := flags.String("in", "", "authoring asset JSON path")
	output := flags.String("out", "", "compiled header path ending in .gkassetc or .gkmodelassetc")
	lod2 := flags.Bool("lod2", false, "emit optional conservative 2x LOD for eligible opaque inline parts")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("assetcompile accepts flags only; unexpected positional arguments")
	}
	if *input == "" || *output == "" {
		return fmt.Errorf("assetcompile requires -in and -out")
	}
	modelOutput := filepath.Ext(*output) == ".gkmodelassetc"
	if !modelOutput && filepath.Ext(*output) != ".gkassetc" {
		return fmt.Errorf("compiled output must end in lowercase .gkassetc or .gkmodelassetc")
	}
	options := gekko.CompiledAssetCompileOptions{EnableLOD2: *lod2}
	var result gekko.CompiledAssetCompileDetailedResult
	var modelsWritten, modelsReused int
	var err error
	if modelOutput {
		var compiled gekko.CompiledAssetModelCompileResult
		compiled, err = gekko.CompileAuthoredModelAssetWithOptions(*input, *output, nil, options)
		result = compiled.CompiledAssetCompileDetailedResult
		modelsWritten, modelsReused = compiled.ModelsWritten, compiled.ModelsReused
	} else {
		result, err = gekko.CompileAuthoredAssetWithOptions(*input, *output, nil, options)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(stdout, "Header written: %t; shapes: %d written, %d reused; dependencies: %d written, %d reused\n", result.HeaderWrote, result.ShapesWritten, result.ShapesReused, result.DependenciesWritten, result.DependenciesReused)
	if err == nil && modelOutput {
		_, err = fmt.Fprintf(stdout, "Models: %d written, %d reused\n", modelsWritten, modelsReused)
	}
	if err == nil && *lod2 {
		_, err = fmt.Fprintf(stdout, "LODs: %d written, %d reused\n", result.LODsWritten, result.LODsReused)
	}
	return err
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "assetcompile:", err)
		os.Exit(1)
	}
}
