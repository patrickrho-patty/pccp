package command

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// Service implements Command Authorization (PRD §17.3).
// Command policy parses rather than string-matches where feasible.
type Service struct {
	mu       sync.RWMutex
	policies map[string]*CommandPolicy // orgID → policy
}

// CommandPolicy defines what commands are allowed/denied.
type CommandPolicy struct {
	OrganizationID       string        `json:"organization_id"`
	AllowRules           []CommandRule `json:"allow_rules"`
	DenyRules            []CommandRule `json:"deny_rules"`
	RequireApprovalRules []CommandRule `json:"require_approval_rules"`
	DefaultBehavior      string        `json:"default_behavior"`
}

// CommandRule defines a parsed command matching rule.
type CommandRule struct {
	ID              string   `json:"id"`
	Executable      string   `json:"executable"`                 // e.g. "pytest", "curl", "git"
	ArgsPattern     string   `json:"args_pattern"`               // regex pattern for arguments
	WorkingDir      string   `json:"working_dir,omitempty"`      // required working directory
	EnvRequired     []string `json:"env_required,omitempty"`     // required env vars
	EnvForbidden    []string `json:"env_forbidden,omitempty"`    // forbidden env vars
	NetworkBehavior string   `json:"network_behavior,omitempty"` // none, outbound, inbound
	PrivilegeLevel  string   `json:"privilege_level,omitempty"`  // user, root
	RiskClass       string   `json:"risk_class"`                 // low, medium, high, critical
	Reason          string   `json:"reason"`
}

// CommandRequest is a parsed command to be evaluated.
type CommandRequest struct {
	OrganizationID  string   `json:"organization_id"`
	SessionID       string   `json:"session_id"`
	Executable      string   `json:"executable"`
	Arguments       []string `json:"arguments"`
	WorkingDir      string   `json:"working_dir"`
	EnvVars         []string `json:"env_vars"` // names only, values not captured for privacy
	RequiresNetwork bool     `json:"requires_network"`
	RequiresRoot    bool     `json:"requires_root"`
}

// CommandDecision is the authorization decision.
type CommandDecision struct {
	Allowed          bool     `json:"allowed"`
	Reason           string   `json:"reason"`
	RequiresApproval bool     `json:"requires_approval"`
	RiskClass        string   `json:"risk_class"`
	MatchedRule      string   `json:"matched_rule,omitempty"`
	RuleIDs          []string `json:"rule_ids,omitempty"`
}

// New creates a new command authorization service.
func New() *Service {
	return &Service{
		policies: make(map[string]*CommandPolicy),
	}
}

// SetPolicy sets the command policy for an organization.
func (s *Service) SetPolicy(orgID string, policy CommandPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[orgID] = &policy
}

// GetPolicy returns the command policy for an organization.
func (s *Service) GetPolicy(orgID string) *CommandPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if p, ok := s.policies[orgID]; ok {
		return p
	}
	return defaultPolicy(orgID)
}

// Evaluate checks whether a command is permitted.
func (s *Service) Evaluate(req CommandRequest) *CommandDecision {
	policy := s.GetPolicy(req.OrganizationID)
	decision := &CommandDecision{
		RiskClass: "low",
	}

	// 1. Check deny rules first (deny takes precedence)
	for _, rule := range policy.DenyRules {
		if matchRule(rule, req) {
			decision.Allowed = false
			decision.Reason = fmt.Sprintf("denied by rule %s: %s", rule.ID, rule.Reason)
			decision.RiskClass = rule.RiskClass
			decision.MatchedRule = rule.ID
			decision.RuleIDs = append(decision.RuleIDs, "cmd.deny."+rule.ID)
			return decision
		}
	}

	// 2. Check if command requires approval
	for _, rule := range policy.RequireApprovalRules {
		if matchRule(rule, req) {
			decision.Allowed = false
			decision.RequiresApproval = true
			decision.Reason = fmt.Sprintf("requires approval per rule %s: %s", rule.ID, rule.Reason)
			decision.RiskClass = rule.RiskClass
			decision.MatchedRule = rule.ID
			decision.RuleIDs = append(decision.RuleIDs, "cmd.approval."+rule.ID)
			return decision
		}
	}

	// 3. Check allow rules
	for _, rule := range policy.AllowRules {
		if matchRule(rule, req) {
			decision.Allowed = true
			decision.Reason = fmt.Sprintf("allowed by rule %s", rule.ID)
			decision.RiskClass = rule.RiskClass
			decision.MatchedRule = rule.ID
			return decision
		}
	}

	// 4. Check inherently dangerous commands
	if isDangerousCommand(req.Executable, req.Arguments) {
		decision.Allowed = false
		decision.RequiresApproval = true
		decision.Reason = "dangerous command requires approval"
		decision.RiskClass = "high"
		decision.RuleIDs = append(decision.RuleIDs, "cmd.dangerous")
		return decision
	}

	// 5. Apply default behavior
	switch policy.DefaultBehavior {
	case "deny":
		decision.Allowed = false
		decision.Reason = "no matching allow rule (default deny)"
		decision.RuleIDs = append(decision.RuleIDs, "cmd.default_deny")
	case "require_approval":
		decision.Allowed = false
		decision.RequiresApproval = true
		decision.Reason = "requires approval (default)"
		decision.RuleIDs = append(decision.RuleIDs, "cmd.default_approval")
	default:
		decision.Allowed = true
		decision.Reason = "allowed (default)"
	}

	return decision
}

// matchRule checks if a command matches a rule.
func matchRule(rule CommandRule, req CommandRequest) bool {
	// Executable match
	if rule.Executable != "" && rule.Executable != req.Executable {
		return false
	}

	// Arguments pattern match
	if rule.ArgsPattern != "" {
		argStr := strings.Join(req.Arguments, " ")
		matched, err := regexp.MatchString(rule.ArgsPattern, argStr)
		if err != nil || !matched {
			return false
		}
	}

	// Working directory check
	if rule.WorkingDir != "" && rule.WorkingDir != req.WorkingDir {
		return false
	}

	// Required env vars
	for _, env := range rule.EnvRequired {
		found := false
		for _, e := range req.EnvVars {
			if e == env {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Forbidden env vars
	for _, env := range rule.EnvForbidden {
		for _, e := range req.EnvVars {
			if e == env {
				return false
			}
		}
	}

	// Network behavior
	if rule.NetworkBehavior == "none" && req.RequiresNetwork {
		return false
	}

	// Privilege level
	if rule.PrivilegeLevel == "user" && req.RequiresRoot {
		return false
	}

	return true
}

// isDangerousCommand checks for inherently dangerous commands.
func isDangerousCommand(exec string, args []string) bool {
	dangerous := map[string]bool{
		"sudo":     true,
		"su":       true,
		"chmod":    true,
		"chown":    true,
		"mkfs":     true,
		"dd":       true,
		"fdisk":    true,
		"shutdown": true,
		"reboot":   true,
		"kill":     true,
		"killall":  true,
	}
	if dangerous[exec] {
		return true
	}

	// Check for destructive git operations
	if exec == "git" {
		for _, arg := range args {
			if arg == "push" && containsArg(args, "--force") {
				return true
			}
			if arg == "reset" && containsArg(args, "--hard") {
				return true
			}
			if arg == "clean" && containsArg(args, "-fd") {
				return true
			}
		}
	}

	// Check for rm with recursive force
	if exec == "rm" {
		for _, arg := range args {
			if arg == "-rf" || arg == "-fr" || (arg == "-r" && containsArg(args, "-f")) {
				return true
			}
		}
	}

	return false
}

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

func defaultPolicy(orgID string) *CommandPolicy {
	return &CommandPolicy{
		OrganizationID:  orgID,
		DefaultBehavior: "allow",
		DenyRules: []CommandRule{
			{ID: "deny-sudo", Executable: "sudo", RiskClass: "critical", Reason: "sudo is prohibited"},
			{ID: "deny-cloud-metadata", Executable: "curl", ArgsPattern: "169\\.254\\.169\\.254", RiskClass: "critical", Reason: "cloud metadata access prohibited"},
		},
		AllowRules: []CommandRule{
			{ID: "allow-test-runners", Executable: "pytest", RiskClass: "low", Reason: "test execution allowed"},
			{ID: "allow-go-test", Executable: "go", ArgsPattern: "^test", RiskClass: "low", Reason: "go test allowed"},
			{ID: "allow-npm", Executable: "npm", ArgsPattern: "^(install|test|run)", RiskClass: "low", Reason: "npm commands allowed"},
			{ID: "allow-git-read", Executable: "git", ArgsPattern: "^(status|log|diff|show|branch)", RiskClass: "low", Reason: "git read operations allowed"},
		},
		RequireApprovalRules: []CommandRule{
			{ID: "approval-git-push", Executable: "git", ArgsPattern: "^push", RiskClass: "medium", Reason: "git push requires approval"},
			{ID: "approval-db-migrate", Executable: "migrate", RiskClass: "high", Reason: "database migration requires approval"},
			{ID: "approval-docker", Executable: "docker", ArgsPattern: "^(build|push|run)", RiskClass: "medium", Reason: "docker operations require approval"},
		},
	}
}
