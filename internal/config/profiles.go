package config

// DeploymentProfile defines the three consumption profiles (PRD §1.1, §34).
type DeploymentProfile struct {
	Name            string `json:"name"` // enterprise, public, sovereign
	DefaultLocale   string `json:"default_locale"`
	DefaultTimezone string `json:"default_timezone"`
	// Security defaults
	RequireMDM         bool   `json:"require_mdm"`
	RequireMFA         bool   `json:"require_mfa"`
	RequireHardwareKey bool   `json:"require_hardware_key"`
	RequireAttestation bool   `json:"require_attestation"`
	MinAssuranceLevel  string `json:"min_assurance_level"`
	// Network
	AllowPublicInternet bool `json:"allow_public_internet"`
	RequireVPN          bool `json:"require_vpn"`
	// Retention
	TranscriptRetention string `json:"transcript_retention"` // metadata_only, redacted, full
	MaxRetentionDays    int    `json:"max_retention_days"`
	// Updates
	UpdateMode string `json:"update_mode"` // automatic, manual, offline
	// Compliance
	KoreanPIIDetection bool   `json:"korean_pii_detection"`
	AuditLevel         string `json:"audit_level"` // standard, enhanced, maximum
}

// EnterpriseProfile returns the default enterprise deployment profile.
func EnterpriseProfile() DeploymentProfile {
	return DeploymentProfile{
		Name:                "enterprise",
		DefaultLocale:       "ko-KR",
		DefaultTimezone:     "Asia/Seoul",
		RequireMFA:          true,
		MinAssuranceLevel:   "L1",
		AllowPublicInternet: true,
		TranscriptRetention: "metadata_only",
		MaxRetentionDays:    90,
		UpdateMode:          "automatic",
		KoreanPIIDetection:  true,
		AuditLevel:          "enhanced",
	}
}

// SovereignProfile returns the government/sovereign deployment profile (PRD §34.1).
func SovereignProfile() DeploymentProfile {
	return DeploymentProfile{
		Name:                "sovereign",
		DefaultLocale:       "ko-KR",
		DefaultTimezone:     "Asia/Seoul",
		RequireMDM:          true,
		RequireMFA:          true,
		RequireHardwareKey:  true,
		RequireAttestation:  true,
		MinAssuranceLevel:   "L2",
		AllowPublicInternet: false,
		RequireVPN:          true,
		TranscriptRetention: "metadata_only",
		MaxRetentionDays:    365,
		UpdateMode:          "offline",
		KoreanPIIDetection:  true,
		AuditLevel:          "maximum",
	}
}

// PublicProfile returns the individual/public deployment profile.
func PublicProfile() DeploymentProfile {
	return DeploymentProfile{
		Name:                "public",
		DefaultLocale:       "ko-KR",
		DefaultTimezone:     "Asia/Seoul",
		MinAssuranceLevel:   "L1",
		AllowPublicInternet: true,
		TranscriptRetention: "redacted",
		MaxRetentionDays:    30,
		UpdateMode:          "automatic",
		KoreanPIIDetection:  true,
		AuditLevel:          "standard",
	}
}

// GetProfile returns the deployment profile by name.
func GetProfile(name string) DeploymentProfile {
	switch name {
	case "sovereign", "government":
		return SovereignProfile()
	case "public", "individual":
		return PublicProfile()
	default:
		return EnterpriseProfile()
	}
}
