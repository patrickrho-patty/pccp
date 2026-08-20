package skillpolicy

import "testing"

func resolveIdentity(identity string, opts ResolveOptions, assignments ...Assignment) Result {
	reported := []ReportedSkill{{Identity: identity, Digest: "abc"}}
	out := Resolve(reported, assignments, opts)
	for _, r := range out {
		if r.SkillIdentity == identity {
			return r
		}
	}
	return Result{}
}

func s(t *testing.T, opts ResolveOptions, assignments ...Assignment) Result {
	return resolveIdentity("s", opts, assignments...)
}

func TestRequiredWinsOverOptionalWhenNotBlocked(t *testing.T) {
	res := s(t, ResolveOptions{EnforcementEnabled: true},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeOrg, State: Optional},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeFleet, State: Required},
	)
	if res.State != Required {
		t.Fatalf("expected Required, got %s (%s)", res.State, res.WinningRule)
	}
	if !res.Approved {
		t.Fatal("expected approved=true for exact digest")
	}
}

func TestBlockedWinsOverRequiredAndOptional(t *testing.T) {
	res := s(t, ResolveOptions{EnforcementEnabled: true},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeOrg, State: Optional},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeTeam, State: Required},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeFleet, State: Blocked},
	)
	if res.State != Blocked {
		t.Fatalf("expected Blocked, got %s", res.State)
	}
}

func TestNarrowerCannotWeakenBroadBlocked(t *testing.T) {
	res := s(t, ResolveOptions{EnforcementEnabled: true},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeOrg, State: Blocked},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeUser, State: Optional},
	)
	if res.State != Blocked {
		t.Fatalf("expected Blocked (broad stands), got %s", res.State)
	}
}

func TestNarrowerStrengthensOptionalToRequired(t *testing.T) {
	res := s(t, ResolveOptions{EnforcementEnabled: true},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeOrg, State: Optional},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeUser, State: Required},
	)
	if res.State != Required {
		t.Fatalf("expected Required, got %s", res.State)
	}
}

func TestUnknownSkillBlockedWhenEnforcementEnabled(t *testing.T) {
	res := resolveIdentity("patch@plugins/sandbox", ResolveOptions{EnforcementEnabled: true})
	if res.State != Blocked {
		t.Fatalf("expected Blocked (no policy), got %s", res.State)
	}
	if !res.Unknown {
		t.Fatal("expected Unknown=true")
	}
}

func TestAuditMigrationObservesUnknownInsteadOfBlocking(t *testing.T) {
	res := resolveIdentity("patch@plugins/sandbox", ResolveOptions{EnforcementEnabled: false})
	if res.State == Blocked {
		t.Fatalf("audit mode must NOT block, got %s", res.State)
	}
	if res.State != Optional {
		t.Fatalf("audit mode observes as optional, got %s", res.State)
	}
}

func TestUnverifiedDigestBlocksEvenWhenIdentityApproved(t *testing.T) {
	reported := []ReportedSkill{{Identity: "s", Digest: "xyz"}}
	assignments := []Assignment{{SkillIdentity: "s", Digest: "abc", Scope: ScopeOrg, State: Required}}
	out := Resolve(reported, assignments, ResolveOptions{EnforcementEnabled: true})
	if out[0].State != Blocked {
		t.Fatalf("unverified digest must block, got %s (%s)", out[0].State, out[0].WinningRule)
	}
	if out[0].Approved {
		t.Fatal("must not be approved for a mismatched digest")
	}
}

func TestSameStateAcrossScopesListsAllContributing(t *testing.T) {
	res := s(t, ResolveOptions{EnforcementEnabled: true},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeOrg, State: Required},
		Assignment{SkillIdentity: "s", Digest: "abc", Scope: ScopeTeam, State: Required},
	)
	if len(res.Contributing) != 2 {
		t.Fatalf("expected 2 contributing scopes, got %d (%v)", len(res.Contributing), res.Contributing)
	}
}