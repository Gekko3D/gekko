package content

import (
	"encoding/json"
	"fmt"
)

type importedWorldJSONAlias ImportedWorldDef

// MarshalJSON keeps the public legacy sector API separate from the indexed v3 wire.
func (d ImportedWorldDef) MarshalJSON() ([]byte, error) {
	type wire struct {
		*importedWorldJSONAlias
		Sectors any `json:"sectors,omitempty"`
	}
	var sectors any
	if d.SchemaVersion == ImportedWorldPageSchemaVersion {
		if len(d.IndexedSectors) > 0 {
			sectors = d.IndexedSectors
		}
	} else if len(d.Sectors) > 0 {
		sectors = d.Sectors
	}
	return json.Marshal(wire{importedWorldJSONAlias: (*importedWorldJSONAlias)(&d), Sectors: sectors})
}

func (d *ImportedWorldDef) UnmarshalJSON(data []byte) error {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	version := 0
	if raw, ok := envelope["schema_version"]; ok {
		if err := json.Unmarshal(raw, &version); err != nil {
			return err
		}
	}
	if !legacyImportedWorldVersion(version) && version != ImportedWorldPageSchemaVersion {
		return fmt.Errorf("unsupported imported world schema version %d", version)
	}
	var decoded importedWorldJSONAlias
	wire := struct {
		*importedWorldJSONAlias
		Sectors json.RawMessage `json:"sectors"`
	}{importedWorldJSONAlias: &decoded}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	var sectors []map[string]json.RawMessage
	if len(wire.Sectors) > 0 {
		if err := json.Unmarshal(wire.Sectors, &sectors); err != nil {
			return err
		}
	}
	forbidden := []string{"full_chunk_indices", "visible_sector_indices", "adjacent_sector_indices"}
	if version == ImportedWorldPageSchemaVersion {
		forbidden = []string{"full_chunk_refs", "visible_sector_refs", "adjacent_sector_refs", "lods"}
	}
	for _, sector := range sectors {
		for _, key := range forbidden {
			if _, present := sector[key]; present {
				return fmt.Errorf("sector reference %s is invalid for imported world schema version %d", key, version)
			}
		}
	}
	if version == ImportedWorldPageSchemaVersion {
		if len(wire.Sectors) > 0 {
			if err := json.Unmarshal(wire.Sectors, &decoded.IndexedSectors); err != nil {
				return err
			}
		}
	} else {
		for _, key := range []string{"pages", "root_page_indices"} {
			if _, present := envelope[key]; present {
				return fmt.Errorf("page field %s requires imported world schema version 3", key)
			}
		}
		if len(wire.Sectors) > 0 {
			if err := json.Unmarshal(wire.Sectors, &decoded.Sectors); err != nil {
				return err
			}
		}
	}
	*d = ImportedWorldDef(decoded)
	return nil
}
