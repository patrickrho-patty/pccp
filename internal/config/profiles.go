package config

// DeploymentProfile defines the three consumption profiles (PRD §1.1, §34).
type DeploymentProfile struct {
	Name             string // enterprise, public, sovereign
	DefaultLocale    string
	DefaultTimezone  string
	// Security defaults
	RequireMDM       bool
	RequireMFA       bool
	RequireHardwareKey bool
	RequireAttestation bool
	MinAssuranceLevel string
	// Network
	AllowPublicInternet bool
	RequireVPN          bool
	// Retention
	TranscriptRetention string // metadata_only, redacted, full
	MaxRetentionDays    int
	// Updates
	UpdateMode          string // automatic, manual, offline
	// Compliance
	KoreanPIIDetection  bool
	AuditLevel          string // standard, enhanced, maximum
}

// EnterpriseProfile returns the default enterprise deployment profile.
func EnterpriseProfile() DeploymentProfile {
	return DeploymentProfile{
		Name:               "enterprise",
		DefaultLocale:      "ko-KR",
		DefaultTimezone:    "Asia/Seoul",
		RequireMFA:         true,
		MinAssuranceLevel:  "L1",
		AllowPublicInternet: true,
		TranscriptRetention: "metadata_only",
		MaxRetentionDays:   90,
		UpdateMode:         "automatic",
		KoreanPIIDetection: true,
		AuditLevel:         "enhanced",
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
		Name:               "public",
		DefaultLocale:      "ko-KR",
		DefaultTimezone:    "Asia/Seoul",
		MinAssuranceLevel:  "L1",
		AllowPublicInternet: true,
		TranscriptRetention: "redacted",
		MaxRetentionDays:   30,
		UpdateMode:         "automatic",
		KoreanPIIDetection: true,
		AuditLevel:         "standard",
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
