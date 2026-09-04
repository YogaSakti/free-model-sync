package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

type modelConfig struct {
	Name             string          `json:"name"`
	Alias            string          `json:"alias"`
	DisplayName      string          `json:"display-name,omitempty"`
	MaxContextLength int             `json:"max-context-length,omitempty"`
	ForceMapping     bool            `json:"force-mapping,omitempty"`
	Image            bool            `json:"image,omitempty"`
	InputModalities  []string        `json:"input-modalities,omitempty"`
	OutputModalities []string        `json:"output-modalities,omitempty"`
	IsCompat         bool            `json:"is-compat,omitempty"`
	Thinking         json.RawMessage `json:"thinking,omitempty"`
}

type modelCatalog struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

func freeModelIDs(raw []byte, suffix string) ([]string, error) {
	suffix = strings.TrimSpace(suffix)
	if suffix == "" {
		return nil, errors.New("free suffix is required")
	}
	var catalog modelCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, errors.New("invalid model catalog")
	}
	seen := make(map[string]struct{}, len(catalog.Data))
	models := make([]string, 0, len(catalog.Data))
	for _, item := range catalog.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || !strings.HasSuffix(id, suffix) {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

func mergeModels(existing []modelConfig, freeIDs []string, suffix string) []modelConfig {
	suffix = strings.TrimSpace(suffix)
	aliases := make(map[string]string)
	seen := make(map[string]struct{}, len(existing)+len(freeIDs))
	merged := make([]modelConfig, 0, len(existing)+len(freeIDs))
	for _, model := range existing {
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" {
			continue
		}
		if suffix != "" && strings.HasSuffix(model.Name, suffix) {
			aliases[model.Name] = model.Alias
			continue
		}
		if _, exists := seen[model.Name]; exists {
			continue
		}
		seen[model.Name] = struct{}{}
		merged = append(merged, model)
	}
	for _, id := range freeIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		alias := aliases[id]
		if alias == "" {
			alias = defaultAlias(id, suffix)
		}
		merged = append(merged, modelConfig{Name: id, Alias: alias})
	}
	return merged
}

func defaultAlias(id, suffix string) string {
	id = strings.TrimSuffix(strings.TrimSpace(id), suffix)
	if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
		id = id[slash+1:]
	}
	return id
}
