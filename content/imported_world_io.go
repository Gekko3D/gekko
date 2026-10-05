package content

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type ImportedWorldChunkSaveOptions struct {
	PayloadKind string
}

type ImportedWorldChunkSaveResult struct {
	Wrote            bool
	PayloadKind      string
	PayloadHash      string
	PayloadSizeBytes int
}

func SaveImportedWorld(path string, def *ImportedWorldDef) error {
	if def == nil {
		return fmt.Errorf("imported world is nil")
	}
	if def.SchemaVersion == ImportedWorldPageSchemaVersion {
		if _, err := ValidateImportedWorldV3(def); err != nil {
			return err
		}
		data, err := json.MarshalIndent(def, "", "  ")
		if err != nil {
			return err
		}
		return writeImportedWorldManifestAtomic(path, data)
	}
	if len(def.Pages) > 0 || len(def.RootPageIndices) > 0 || len(def.IndexedSectors) > 0 {
		return fmt.Errorf("imported world page fields require schema version 3")
	}
	EnsureImportedWorldDefaults(def)
	if def.SchemaVersion != CurrentImportedWorldSchemaVersion {
		return fmt.Errorf("unsupported imported world schema version %d", def.SchemaVersion)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func LoadImportedWorld(path string) (*ImportedWorldDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var def ImportedWorldDef
	if err := json.Unmarshal(data, &def); err != nil {
		return nil, err
	}
	if def.SchemaVersion == ImportedWorldPageSchemaVersion {
		index, err := ValidateImportedWorldV3(&def)
		if err != nil {
			return nil, err
		}
		def.PageIndex = index
		return &def, nil
	}
	index, err := NormalizeImportedWorldPages(&def)
	if err != nil {
		return nil, err
	}
	def.SchemaVersion = CurrentImportedWorldSchemaVersion
	EnsureImportedWorldDefaults(&def)
	def.PageIndex = index
	return &def, nil
}

func SaveImportedWorldChunk(path string, def *ImportedWorldChunkDef) error {
	return SaveImportedWorldChunkWithOptions(path, def, ImportedWorldChunkSaveOptions{
		PayloadKind: ImportedWorldChunkPayloadSparseJSONV1,
	})
}

func SaveImportedWorldChunkWithOptions(path string, def *ImportedWorldChunkDef, opts ImportedWorldChunkSaveOptions) error {
	_, err := SaveImportedWorldChunkWithOptionsResult(path, def, opts)
	return err
}

func SaveImportedWorldChunkWithOptionsResult(path string, def *ImportedWorldChunkDef, opts ImportedWorldChunkSaveOptions) (ImportedWorldChunkSaveResult, error) {
	if def == nil {
		return ImportedWorldChunkSaveResult{}, fmt.Errorf("imported world chunk is nil")
	}
	EnsureImportedWorldChunkDefaults(def)
	if def.SchemaVersion != CurrentImportedWorldChunkSchemaVersion {
		return ImportedWorldChunkSaveResult{}, fmt.Errorf("unsupported imported world chunk schema version %d", def.SchemaVersion)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	payloadKind, err := NormalizeImportedWorldChunkPayloadKind(opts.PayloadKind)
	if err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	def.PayloadKind = payloadKind
	if payloadKind == ImportedWorldChunkPayloadBrickZstdBinaryV1 {
		codec, err := defaultImportedCompiledCodec()
		if err != nil {
			return ImportedWorldChunkSaveResult{}, err
		}
		return SaveImportedWorldChunkCompiledWithCodec(path, def, codec)
	}
	if payloadKind == ImportedWorldChunkPayloadDenseRLEBinaryV1 {
		return saveImportedWorldChunkDenseRLEBinary(path, def)
	}
	def.PayloadHash = ""
	def.PayloadSizeBytes = 0
	data, err := json.MarshalIndent(def, "", "  ")
	if err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	wrote, err := writeFileIfChanged(path, data, 0644)
	if err != nil {
		return ImportedWorldChunkSaveResult{}, err
	}
	return ImportedWorldChunkSaveResult{
		Wrote:            wrote,
		PayloadKind:      def.PayloadKind,
		PayloadHash:      def.PayloadHash,
		PayloadSizeBytes: def.PayloadSizeBytes,
	}, nil
}

func LoadImportedWorldChunk(path string) (*ImportedWorldChunkDef, error) {
	return LoadImportedWorldChunkWithCodec(path, nil)
}

func ResolveImportedWorldChunkPath(entry ImportedWorldChunkEntryDef, manifestPath string) string {
	return ResolveDocumentPath(entry.ChunkPath, manifestPath)
}

func writeFileIfChanged(path string, data []byte, perm os.FileMode) (bool, error) {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, data) {
		return false, nil
	}
	if err != nil && !os.IsNotExist(err) {
		return false, err
	}
	if err := os.WriteFile(path, data, perm); err != nil {
		return false, err
	}
	return true, nil
}

func writeImportedWorldManifestAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".gkworld-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if err = file.Chmod(0644); err != nil {
		file.Close()
		return err
	}
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), path)
}
