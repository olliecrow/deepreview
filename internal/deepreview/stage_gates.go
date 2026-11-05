package deepreview

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

type triageDecision struct {
	NoOp  bool         `json:"no_op"`
	Items []triageItem `json:"items"`
}

type triageItem struct {
	ID          string `json:"id"`
	Disposition string `json:"disposition"`
	Impact      string `json:"impact"`
	Confidence  string `json:"confidence"`
	Evidence    string `json:"evidence"`
}

func validateReviewReport(path string) error {
	body, err := readFileFromRoot(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return NewDeepReviewError("independent review report missing: %s", path)
	}
	return validateReviewReportText(string(body))
}

func validateReviewReportText(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return NewDeepReviewError("independent review report is empty")
	}
	if !strings.Contains(text, "material_findings_found: yes") && !strings.Contains(text, "material_findings_found: no") {
		return NewDeepReviewError("independent review report missing material_findings_found verdict")
	}
	if !strings.Contains(text, "merge_readiness: ready") && !strings.Contains(text, "merge_readiness: needs_fixes") {
		return NewDeepReviewError("independent review report missing merge_readiness verdict")
	}
	return nil
}

func validateTriageDecisionFile(path string) error {
	body, err := readFileFromRoot(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return NewDeepReviewError("triage decision missing: %s", path)
	}
	if strings.TrimSpace(string(body)) == "" {
		return NewDeepReviewError("triage decision is empty: %s", path)
	}
	var decision triageDecision
	if err := json.Unmarshal(body, &decision); err != nil {
		return NewDeepReviewError("triage decision is malformed: %s", path)
	}
	if err := validateTriageDecision(decision); err != nil {
		return err
	}
	return nil
}

func validateTriageDecision(decision triageDecision) error {
	if decision.NoOp {
		if len(decision.Items) != 0 {
			return NewDeepReviewError("triage no_op decision must have an empty item list")
		}
		return nil
	}
	if len(decision.Items) == 0 {
		return NewDeepReviewError("triage decision has no items and is not an explicit no_op")
	}
	seen := map[string]struct{}{}
	for _, item := range decision.Items {
		id := strings.TrimSpace(item.ID)
		if id == "" || strings.Contains(id, "/") || strings.Contains(id, "..") || !triageIDPattern(id) {
			return NewDeepReviewError("triage item id must be one safe segment")
		}
		if _, ok := seen[id]; ok {
			return NewDeepReviewError("triage item id %s is duplicated", id)
		}
		seen[id] = struct{}{}
		disposition := strings.ToLower(strings.TrimSpace(item.Disposition))
		switch disposition {
		case "accept", "reject", "defer":
		default:
			return NewDeepReviewError("triage item %s has invalid disposition", id)
		}
		if disposition != "accept" {
			continue
		}
		if strings.ToLower(strings.TrimSpace(item.Impact)) != "material" ||
			strings.ToLower(strings.TrimSpace(item.Confidence)) != "high" ||
			strings.TrimSpace(item.Evidence) == "" {
			return NewDeepReviewError("accepted triage item %s requires impact material, confidence high, and evidence", id)
		}
	}
	return nil
}

func triageIDPattern(id string) bool {
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return id != "" && id != "." && id != ".."
}

func persistValidatedTriageAndPlan(srcRoot, dstRoot string) error {
	copies := []struct {
		srcRel  string
		dstRel  string
		missing string
	}{
		{filepath.Join(".deepreview", "artifacts", "round-triage.md"), "round-triage.md", "execute prompt 1 triage markdown missing"},
		{filepath.Join(".deepreview", "artifacts", "round-triage.json"), "round-triage.json", "execute prompt 1 triage decision missing"},
		{filepath.Join(".deepreview", "artifacts", "round-plan.md"), "round-plan.md", "execute prompt 1 plan missing"},
	}
	for _, item := range copies {
		if err := copyBoundedArtifact(dstRoot, item.dstRel, srcRoot, item.srcRel); err != nil {
			return NewDeepReviewError("%s", item.missing)
		}
	}
	if err := validateTriageDecisionFile(filepath.Join(dstRoot, "round-triage.json")); err != nil {
		return err
	}
	body, err := readFileFromRoot(dstRoot, "round-plan.md")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(body)) == "" {
		return NewDeepReviewError("execute prompt 1 plan is empty")
	}
	return nil
}
