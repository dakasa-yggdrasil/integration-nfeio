package adapter

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"
)

// ManageTemplateInput is the polymorphic envelope for manage_template.
// Operation must be one of: get, list, validate. v1.0.0 is read-only —
// no create/update/delete operations.
type ManageTemplateInput struct {
	Operation     string `json:"operation"`
	MunicipioCode string `json:"municipio_code,omitempty"`
	YAMLContent   string `json:"yaml_content,omitempty"`
}

// ManageTemplateOutput carries the union of get/list/validate results.
// Only the fields appropriate for the requested operation are populated.
type ManageTemplateOutput struct {
	Template  *MunicipioTemplate `json:"template,omitempty"`
	Templates []TemplateSummary  `json:"templates,omitempty"`
	Valid     bool               `json:"valid,omitempty"`
	Errors    []string           `json:"errors,omitempty"`
}

// TemplateSummary is the compact list view (used by Operation="list").
type TemplateSummary struct {
	MunicipioCode string  `json:"municipio_code"`
	Name          string  `json:"name"`
	ISSRate       float64 `json:"iss_rate"`
	SpecialRegime string  `json:"special_regime"`
}

// ManageTemplate exposes the in-memory municipality template catalog as a
// read-only RPC. get fetches one template; list returns summaries; validate
// runs the same schema validator the loader uses but against a candidate
// YAML provided by the caller.
func ManageTemplate(ctx context.Context, templates map[string]*MunicipioTemplate, in ManageTemplateInput) (*ManageTemplateOutput, error) {
	switch in.Operation {
	case "get":
		tpl, ok := templates[in.MunicipioCode]
		if !ok {
			return nil, fmt.Errorf("template_not_found: %s", in.MunicipioCode)
		}
		return &ManageTemplateOutput{Template: tpl}, nil
	case "list":
		out := &ManageTemplateOutput{}
		for code, tpl := range templates {
			out.Templates = append(out.Templates, TemplateSummary{
				MunicipioCode: code, Name: tpl.Name, ISSRate: tpl.ISS.Rate, SpecialRegime: tpl.ISS.SpecialRegime,
			})
		}
		return out, nil
	case "validate":
		tpl := &MunicipioTemplate{}
		if err := yaml.Unmarshal([]byte(in.YAMLContent), tpl); err != nil {
			return &ManageTemplateOutput{Valid: false, Errors: []string{err.Error()}}, nil
		}
		if err := validateTemplate(tpl, "(input)"); err != nil {
			return &ManageTemplateOutput{Valid: false, Errors: []string{err.Error()}}, nil
		}
		return &ManageTemplateOutput{Valid: true}, nil
	default:
		return nil, fmt.Errorf("unsupported operation %q (allowed: get, list, validate)", in.Operation)
	}
}
