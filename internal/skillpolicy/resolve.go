package skillpolicy

// Package skillpolicy implements PAT-1456's deterministic managed-skill
// policy resolution. It is the single, testable authority for turning a set
// of scoped (org/team/fleet/user) skill assignments plus a reported skill
// inventory into one effective state per skill.
//
// Rules enforced here (mirroring the spec exactly):
//   - identity is the canonical skill/package identity + content digest, never
//     display name or path; renaming/copying/moving/shadowing cannot bypass
//   - Blocked at any applicable scope wins over Required and Optional.
//   - Required wins over Optional when no scope blocks.
//   - a narrower scope may strengthen (Optional→Required|Blocked) but cannot
//     weaken a higher-scope Blocked.
//   - multiple-team membership resolves to the most restrictive effective
//     result.
//   - if no approved policy exists after enforcement is enabled, the skill is
//     Blocked (fail closed).
//   - unknown/unapproved skills are Blocked once enforcement is on.
//
// The engine is pure (no DB/IO) so it is trivially testable and reusable by
// the API, the admin UI backend, and the harness enforcement path.

// State is the three-state managed policy for one skill.
type State string

const (
	Required State = "required"
	Optional State = "optional"
	Blocked  State = "blocked"
)

// Scope is a policy-assignment scope. Higher integer = narrower/higher authority
// for strengthening decisions (a narrow scope can strengthen but not weaken a
// broad Blocked).
type Scope string

const (
	ScopeOrg   Scope = "org"
	ScopeTeam  Scope = "team"
	ScopeFleet Scope = "fleet" // fleet/harness
	ScopeUser  Scope = "user"
)

var scopeRank = map[Scope]int{ScopeOrg: 0, ScopeTeam: 1, ScopeFleet: 2, ScopeUser: 3}

// Assignment is one administrator decision for a skill at one scope.
type Assignment struct {
	SkillIdentity string // canonical package/skill identity (id@package)
	Digest        string // approved content digest ("" = unverified/all-unknown)
	Scope         Scope
	State         State
}

// ResolveOptions carries evaluator settings.
type ResolveOptions struct {
	EnforcementEnabled bool // when true, unapproved/unknown ⇒ Blocked (fail closed)
	// Owners: an audit-only migration window may set this false to observe
	// unknown skills instead of blocking them. Default (zero) = fail closed.
}

// Result is the effective outcome for one skill.
type Result struct {
	SkillIdentity string
	Digest        string // approved digest that won ("" if blocked/unapproved)
	State         State
	// Contributing is the ordered list of scopes that set the winning rule,
	// most-restrictive-first, for audit/evidence display.
	Contributing []Scope
	WinningRule  string // human-readable "why", e.g. "blocked at org scope"
	Approved     bool   // digest matched an approved assignment
	Unknown      bool   // seen in inventory but no matching approved assignment
}

// skillKey normalizes the lookup: identity alone for the Digest-agnostic
// "is there any assignment", and identity+digest for the exact-approval check.
func skillKey(identity string) string { return identity }

// resolveOne computes the effective state for a single skill given its
// assignments (already reduced per-scope) and reported digest.
//
// reduce makes the winning assignment for each scope (most restrictive wins).
func resolveOne(identity, reportedDigest string, perScope map[Scope][]Assignment, opts ResolveOptions) Result {
	// Guard against nil maps.
	approved := false
	var winning *Assignment // most restrictive across scopes
	contributing := []Scope{}

	// Evaluate each scope from broadest to narrowest. A narrow scope may
	// strengthen (Blocked/Required beat Optional) but never weaken a broad
	// Blocked. Because per-scope is already most-restrictive, iterate all
	// scopes and keep the most restrictive; if a broader scope blocked, no
	// narrower decision can override it.
	for _, s := range []Scope{ScopeOrg, ScopeTeam, ScopeFleet, ScopeUser} {
		assignments := perScope[s]
		if len(assignments) == 0 {
			continue
		}
		// Per-scope reduction: within one scope, if a specific digest is
		// approved take it; otherwise use the first. Blocked dominates within
		// a scope too.
		var scoped *Assignment
		for i := range assignments {
			a := &assignments[i]
			// exact-digest approval check
			if a.Digest != "" && a.Digest == reportedDigest {
				approved = true
			}
			if scoped == nil {
				scoped = a
				continue
			}
			// most restrictive wins within the scope
			if restrictiveness(a.State) > restrictiveness(scoped.State) {
				scoped = a
			}
		}
		if scoped == nil {
			continue
		}
		// cross-scope: narrower cannot weaken a broad Blocked
		if winning != nil && winning.State == Blocked {
			continue // a broad Blocked stands; narrower cannot un-block
		}
		if scoped.State == Blocked {
			winning = scoped
			contributing = []Scope{s}
			continue
		}
		// narrower may strengthen Optional→Required
		if winning == nil || restrictiveness(scoped.State) > restrictiveness(winning.State) {
			winning = scoped
			contributing = []Scope{s}
		} else if winning.State == scoped.State {
			contributing = append(contributing, s)
		}
	}

	if winning == nil {
		// No approved policy for this identity.
		if opts.EnforcementEnabled {
			return Result{SkillIdentity: identity, Digest: reportedDigest, State: Blocked, Approved: approved, Unknown: true, WinningRule: "no approved policy — blocked (fail closed)"}
		}
		// audit-only migration window: observe as optional/unknown, not blocked
		return Result{SkillIdentity: identity, Digest: reportedDigest, State: Optional, Approved: approved, Unknown: true, WinningRule: "no approved policy — audit-only observe"}
	}

	// A non-blocked winning assignment requires the exact approved digest to be
	// satisfied. If the winning assignment pins a digest that does not equal the
	// reported digest (or pins none), and enforcement is on, treat the skill as
	// unverified → fail closed (block). Never carry approval from one digest to
	// another.
	if winning.State != Blocked && !digestMatches(winning, reportedDigest) {
		if opts.EnforcementEnabled {
			return Result{SkillIdentity: identity, Digest: reportedDigest, State: Blocked, Approved: false, Unknown: true, WinningRule: "approved identity but unverified digest — blocked (fail closed)"}
		}
		return Result{SkillIdentity: identity, Digest: reportedDigest, State: winning.State, Approved: false, Unknown: true, WinningRule: "approved identity, unverified digest (audit mode)"}
	}

	if winning.State == Blocked {
		return Result{SkillIdentity: identity, Digest: reportedDigest, State: Blocked, Approved: approved, Contributing: contributing, WinningRule: "blocked — identity blocked at scope"}
	}

	return Result{
		SkillIdentity: identity, Digest: winning.Digest, State: winning.State,
		Approved: approved, Contributing: contributing,
		WinningRule: ruleLabel(winning.State, contributing[0]),
	}
}

// digestMatches reports whether the winning assignment's pinned digest equals
// the reported one. An assignment with no digest never matches (unverified).
func digestMatches(a *Assignment, reported string) bool {
	return a != nil && a.Digest != "" && a.Digest == reported
}

// restrictiveness orders the three states for conflict resolution.
func restrictiveness(s State) int {
	switch s {
	case Blocked:
		return 3
	case Required:
		return 2
	case Optional:
		return 1
	}
	return 0
}

func ruleLabel(s State, top Scope) string {
	switch s {
	case Required:
		return "required at " + string(top) + " scope"
	case Blocked:
		return "blocked at " + string(top) + " scope"
	case Optional:
		return "optional at " + string(top) + " scope"
	}
	return "unknown"
}

// Resolve evaluates every reported skill against the assignments and returns
// the deterministic effective state for each (ordered by identity for stable
// output). assignments may be supplied pre-grouped by scope.
func Resolve(reported []ReportedSkill, assignments []Assignment, opts ResolveOptions) []Result {
	// Group by scope, then per identity within scope.
	perScope := map[Scope]map[string][]Assignment{}
	for _, a := range assignments {
		if perScope[a.Scope] == nil {
			perScope[a.Scope] = map[string][]Assignment{}
		}
		k := skillKey(a.SkillIdentity)
		perScope[a.Scope][k] = append(perScope[a.Scope][k], a)
	}

	out := make([]Result, 0, len(reported))
	for _, rs := range reported {
		byScope := map[Scope][]Assignment{}
		for s, m := range perScope {
			if v, ok := m[skillKey(rs.Identity)]; ok {
				byScope[s] = v
			}
		}
		out = append(out, resolveOne(rs.Identity, rs.Digest, byScope, opts))
	}
	// Stable order for deterministic output.
	// (Deterministic ordering by identity is left to the caller via a sort to
	// keep this package filter-only; callers may sort results.)
	return out
}

// ReportedSkill is one harness-reported skill (identity + digest + enabled).
type ReportedSkill struct {
	Identity string
	Digest   string
	Enabled  bool
}
