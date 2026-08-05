package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Plannotator resolves its approved text through config.json, so matching only
// the shipped default turns every approval into a deny once it's customised.
func approvedMarkers() []string {
	markers := []string{reviewApprovedMarker}

	body, err := os.ReadFile(filepath.Join(plannotatorDataDir(), "config.json"))
	if err != nil {
		return markers
	}
	var cfg struct {
		Prompts struct {
			Review struct {
				Approved string `json:"approved"`
				Runtimes map[string]struct {
					Approved string `json:"approved"`
				} `json:"runtimes"`
			} `json:"review"`
		} `json:"prompts"`
	}
	if json.Unmarshal(body, &cfg) != nil {
		return markers
	}

	// An empty override is not a marker — it would match every review.
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			markers = append(markers, s)
		}
	}
	add(cfg.Prompts.Review.Approved)
	for _, runtime := range cfg.Prompts.Review.Runtimes {
		add(runtime.Approved)
	}
	return markers
}

func isApproved(out string) bool {
	for _, marker := range approvedMarkers() {
		if strings.Contains(out, marker) {
			return true
		}
	}
	return false
}
