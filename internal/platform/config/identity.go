package config

import "strings"

type IdentityConfig struct {
	GoogleClientID          string
	GoogleAuthorizedParties []string
}

// Missing Google configuration disables only provider exchange, never installs a
// test verifier. OAuth client IDs are public identifiers, not bearer secrets.
func LoadIdentity(lookup LookupEnv) (IdentityConfig, error) {
	if lookup == nil {
		return IdentityConfig{}, invalid("GOOGLE_CLIENT_ID", "lookup required")
	}
	id, _ := lookup("GOOGLE_CLIENT_ID")
	valid := func(s string) bool {
		if len(s) > 256 || !strings.HasSuffix(s, ".apps.googleusercontent.com") {
			return false
		}
		for _, r := range s {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
				return false
			}
		}
		return true
	}
	if id == "" {
		if parties, _ := lookup("GOOGLE_AUTHORIZED_PARTY_IDS"); parties != "" {
			return IdentityConfig{}, invalid("GOOGLE_AUTHORIZED_PARTY_IDS", "requires audience")
		}
		return IdentityConfig{}, nil
	}
	if !valid(id) {
		return IdentityConfig{}, invalid("GOOGLE_CLIENT_ID", "must be an OAuth client ID")
	}
	cfg := IdentityConfig{GoogleClientID: id, GoogleAuthorizedParties: []string{id}}
	if raw, ok := lookup("GOOGLE_AUTHORIZED_PARTY_IDS"); ok && raw != "" {
		ids := strings.Split(raw, ",")
		if len(ids) > 8 {
			return IdentityConfig{}, invalid("GOOGLE_AUTHORIZED_PARTY_IDS", "at most eight allowed")
		}
		for _, party := range ids {
			if !valid(party) {
				return IdentityConfig{}, invalid("GOOGLE_AUTHORIZED_PARTY_IDS", "must contain OAuth client IDs")
			}
			cfg.GoogleAuthorizedParties = append(cfg.GoogleAuthorizedParties, party)
		}
	}
	return cfg, nil
}
