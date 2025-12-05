package interactive

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/joshyorko/rcc/common"
)

// ServerProfile represents a saved remote server configuration
type ServerProfile struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	AuthToken  string `json:"auth_token,omitempty"`
	SkipSSL    bool   `json:"skip_ssl,omitempty"`
	IsDefault  bool   `json:"is_default,omitempty"`
	LastUsed   string `json:"last_used,omitempty"`
	LastStatus string `json:"last_status,omitempty"`
}

// ServerProfiles holds all saved server profiles
type ServerProfiles struct {
	Profiles []ServerProfile `json:"profiles"`
}

// ProfilesFilePath returns the path to the profiles file
func ProfilesFilePath() string {
	return filepath.Join(common.Product.Home(), "remote-servers.json")
}

// LoadServerProfiles loads saved server profiles from disk
func LoadServerProfiles() (*ServerProfiles, error) {
	profiles := &ServerProfiles{
		Profiles: []ServerProfile{},
	}

	data, err := os.ReadFile(ProfilesFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return profiles, nil
		}
		return profiles, err
	}

	err = json.Unmarshal(data, profiles)
	return profiles, err
}

// SaveServerProfiles saves server profiles to disk
func SaveServerProfiles(profiles *ServerProfiles) error {
	data, err := json.MarshalIndent(profiles, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ProfilesFilePath(), data, 0644)
}

// AddProfile adds or updates a server profile
func (p *ServerProfiles) AddProfile(profile ServerProfile) {
	// Check if profile with same name exists
	for i, existing := range p.Profiles {
		if existing.Name == profile.Name {
			p.Profiles[i] = profile
			return
		}
	}
	p.Profiles = append(p.Profiles, profile)
}

// RemoveProfile removes a server profile by name
func (p *ServerProfiles) RemoveProfile(name string) {
	for i, profile := range p.Profiles {
		if profile.Name == name {
			p.Profiles = append(p.Profiles[:i], p.Profiles[i+1:]...)
			return
		}
	}
}

// GetDefault returns the default profile, or nil if none
func (p *ServerProfiles) GetDefault() *ServerProfile {
	for i := range p.Profiles {
		if p.Profiles[i].IsDefault {
			return &p.Profiles[i]
		}
	}
	return nil
}

// SetDefault sets a profile as the default
func (p *ServerProfiles) SetDefault(name string) {
	for i := range p.Profiles {
		p.Profiles[i].IsDefault = p.Profiles[i].Name == name
	}
}
