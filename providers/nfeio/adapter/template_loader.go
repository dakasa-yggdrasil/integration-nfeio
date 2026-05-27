package adapter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MunicipioTemplate is the in-memory representation of one
// manifest/templates/<ibge>.yaml file. Templates capture the municipal
// service-code + ISS-rate matrix that NFe.io needs per município to
// emit a NFSe; we bundle five Brazilian municípios in v1.0.0.
type MunicipioTemplate struct {
	APIVersion       string           `yaml:"apiVersion"`
	MunicipioCode    string           `yaml:"municipio_code"`
	Name             string           `yaml:"name"`
	State            string           `yaml:"state"`
	ServiceCodes     ServiceCodes     `yaml:"service_codes"`
	ISS              ISSConfig        `yaml:"iss"`
	LocationDefaults LocationDefaults `yaml:"location_defaults"`
	RpsSerialNumber  string           `yaml:"rps_serial_number"`
	Notes            string           `yaml:"notes"`
}

// ServiceCodes carries the municipal service-classification fields that
// vary per município.
type ServiceCodes struct {
	CityServiceCode    string `yaml:"city_service_code"`
	FederalServiceCode string `yaml:"federal_service_code"`
	CnaeCode           string `yaml:"cnae_code"`
	NbsCode            string `yaml:"nbs_code"`
}

// ISSConfig carries the ISS rate (between 0 and 1) plus the taxation type
// and any special regime declaration.
type ISSConfig struct {
	Rate          float64 `yaml:"rate"`
	TaxationType  string  `yaml:"taxation_type"`
	SpecialRegime string  `yaml:"special_regime"`
}

// LocationDefaults provides borrower-address defaults when the caller omits
// them on issue_nfse.
type LocationDefaults struct {
	Country  string `yaml:"country"`
	CityCode string `yaml:"city_code"`
	CityName string `yaml:"city_name"`
}

// LoadTemplatesDir scans dir for *.yaml files, parses each as a
// MunicipioTemplate, validates required fields, and returns the populated
// map keyed by MunicipioCode. Any malformed YAML or missing required field
// returns an error; the caller (main.go) treats that as fatal at startup.
func LoadTemplatesDir(dir string) (map[string]*MunicipioTemplate, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read templates dir %s: %w", dir, err)
	}
	out := make(map[string]*MunicipioTemplate)
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".yaml") {
			continue
		}
		path := filepath.Join(dir, ent.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		tpl := &MunicipioTemplate{}
		if err := yaml.Unmarshal(data, tpl); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := validateTemplate(tpl, path); err != nil {
			return nil, err
		}
		out[tpl.MunicipioCode] = tpl
	}
	return out, nil
}

// LoadTemplatesFS is the io/fs.FS variant of LoadTemplatesDir. Useful for
// embedded templates baked into the binary via //go:embed. dir is "." when
// the FS is already scoped to the templates directory.
func LoadTemplatesFS(fsys fs.FS, dir string) (map[string]*MunicipioTemplate, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("read templates fs %s: %w", dir, err)
	}
	out := make(map[string]*MunicipioTemplate)
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".yaml") {
			continue
		}
		path := ent.Name()
		if dir != "." && dir != "" {
			path = dir + "/" + ent.Name()
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		tpl := &MunicipioTemplate{}
		if err := yaml.Unmarshal(data, tpl); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if err := validateTemplate(tpl, path); err != nil {
			return nil, err
		}
		out[tpl.MunicipioCode] = tpl
	}
	return out, nil
}

func validateTemplate(tpl *MunicipioTemplate, path string) error {
	if tpl.MunicipioCode == "" {
		return fmt.Errorf("%s: municipio_code required", path)
	}
	if tpl.Name == "" {
		return fmt.Errorf("%s: name required", path)
	}
	if tpl.State == "" {
		return fmt.Errorf("%s: state required", path)
	}
	if tpl.ServiceCodes.CityServiceCode == "" {
		return fmt.Errorf("%s: service_codes.city_service_code required", path)
	}
	if tpl.ServiceCodes.FederalServiceCode == "" {
		return fmt.Errorf("%s: service_codes.federal_service_code required", path)
	}
	if tpl.ISS.Rate <= 0 || tpl.ISS.Rate > 1 {
		return fmt.Errorf("%s: iss.rate must be in (0, 1]; got %f", path, tpl.ISS.Rate)
	}
	return nil
}
