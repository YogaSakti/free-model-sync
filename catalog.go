package main

import (
	"encoding/json"
	"errors"
	"sort"
	"strconv"
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

type catalogModel struct {
	ID      string          `json:"id"`
	Pricing json.RawMessage `json:"pricing"`
}

type modelCatalog struct {
	Data []catalogModel `json:"data"`
}

func freeModelIDs(raw []byte) ([]string, error) {
	var catalog modelCatalog
	if err := json.Unmarshal(raw, &catalog); err != nil {
		return nil, errors.New("invalid model catalog")
	}
	seen := make(map[string]struct{}, len(catalog.Data))
	models := make([]string, 0, len(catalog.Data))
	for _, item := range catalog.Data {
		id := strings.TrimSpace(item.ID)
		if id == "" || (!hasFreeToken(id) && !hasZeroPricing(item.Pricing)) {
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

func hasFreeToken(id string) bool {
	parts := strings.FieldsFunc(strings.ToLower(id), func(r rune) bool {
		switch r {
		case '-', '_', ':', '/', '.':
			return true
		default:
			return false
		}
	})
	for _, part := range parts {
		if part == "free" {
			return true
		}
	}
	return false
}

func hasZeroPricing(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var pricing map[string]json.RawMessage
	if json.Unmarshal(raw, &pricing) != nil {
		return false
	}
	checked := false
	for _, key := range []string{"prompt", "completion", "input", "output", "request", "image"} {
		valueRaw, exists := pricing[key]
		if !exists {
			continue
		}
		checked = true
		var text string
		if json.Unmarshal(valueRaw, &text) != nil {
			return false
		}
		value, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || value != 0 {
			return false
		}
	}
	return checked
}

func mergeModels(existing []modelConfig, previousManaged, freeIDs []string) []modelConfig {
	managed := make(map[string]struct{}, len(previousManaged))
	for _, id := range previousManaged {
		managed[strings.TrimSpace(id)] = struct{}{}
	}
	aliases := make(map[string]string)
	seen := make(map[string]struct{}, len(existing)+len(freeIDs))
	merged := make([]modelConfig, 0, len(existing)+len(freeIDs))
	for _, model := range existing {
		model.Name = strings.TrimSpace(model.Name)
		model.Alias = strings.TrimSpace(model.Alias)
		if model.Name == "" {
			continue
		}
		if _, owned := managed[model.Name]; owned || hasFreeToken(model.Name) {
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
			alias = defaultAlias(id)
		}
		merged = append(merged, modelConfig{Name: id, Alias: alias})
	}
	return merged
}

func defaultAlias(id string) string {
	id = strings.TrimSpace(id)
	if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
		id = id[slash+1:]
	}
	for _, suffix := range []string{":free", "-free", "_free", ".free"} {
		id = strings.TrimSuffix(id, suffix)
	}
	return id
}
