package relay

import ()

// governance_state.go is the relay-side governance-state snapshot
// push (harness plans C3/C4/D1/D3-D6/E4 production wiring): at
// session setup the relay sends ONE snapshot carrying the org's
// workflow gates (freeze, recalls, acks, version, standards), the
// tool/MCP approval registry, and the sandbox policies. The connector
// decodes it into its governed.State and the gates fire on real tool
// calls.

// wireGovernanceState is the snapshot body (CBOR labels 1..8).
type wireGovernanceState struct {
	Version    uint16                   `cbor:"1,keyasint"`
	OrgID      string                   `cbor:"2,keyasint"`
	RepoID     string                   `cbor:"3,keyasint,omitempty"`
	ModelID    string                   `cbor:"4,keyasint,omitempty"`
	Freeze     *wireGovernanceFreeze    `cbor:"5,keyasint,omitempty"`
	Recalls    []wireGovernanceRecall   `cbor:"6,keyasint,omitempty"`
	Acks       []wireGovernanceAck      `cbor:"7,keyasint,omitempty"`
	VersionReq *wireGovernanceVersion   `cbor:"8,keyasint,omitempty"`
	Standards  []wireGovernanceStandard `cbor:"9,keyasint,omitempty"`
	Tools      []wireGovernanceTool     `cbor:"10,keyasint,omitempty"`
	Sandboxes  []wireGovernanceSandbox  `cbor:"11,keyasint,omitempty"`
	Desired    []wireFleetDesiredState  `cbor:"12,keyasint,omitempty"`
}

type wireGovernanceFreeze struct {
	Reason         string   `cbor:"1,keyasint"`
	ReasonKo       string   `cbor:"2,keyasint"`
	AffectedRepos  []string `cbor:"3,keyasint"`
	AllowedActions []string `cbor:"4,keyasint"`
	NotAfterMs     int64    `cbor:"5,keyasint"`
}

type wireGovernanceRecall struct {
	Model       string `cbor:"1,keyasint"`
	Reason      string `cbor:"2,keyasint"`
	Replacement string `cbor:"3,keyasint"`
}

type wireGovernanceAck struct {
	PolicyEpochID string `cbor:"1,keyasint"`
	SummaryKo     string `cbor:"2,keyasint"`
	Blocking      bool   `cbor:"3,keyasint"`
}

type wireGovernanceVersion struct {
	MinVersion string `cbor:"1,keyasint"`
	Ring       string `cbor:"2,keyasint"`
}

type wireGovernanceStandard struct {
	RuleID        string `cbor:"1,keyasint"`
	BlockPattern  string `cbor:"2,keyasint"`
	Description   string `cbor:"3,keyasint"`
	DescriptionKo string `cbor:"4,keyasint"`
}

type wireGovernanceTool struct {
	ToolID string `cbor:"1,keyasint"`
	Status string `cbor:"2,keyasint"`
}

type wireGovernanceSandbox struct {
	RepositoryID string `cbor:"1,keyasint"`
	Mode         string `cbor:"2,keyasint"`
	RiskClass    string `cbor:"3,keyasint"`
}

type wireFleetDesiredState struct {
	Action     string `cbor:"1,keyasint"`
	Parameters string `cbor:"2,keyasint,omitempty"`
}

// GovernanceStateView is the projection the relay gathers from its
// services to build the snapshot.
type GovernanceStateView struct {
	OrgID      string
	RepoID     string
	ModelID    string
	Freeze     *GovernanceFreezeView
	Recalls    []GovernanceRecallView
	Acks       []GovernanceAckView
	VersionReq *GovernanceVersionView
	Standards  []GovernanceStandardView
	Tools      []GovernanceToolView
	Sandboxes  []GovernanceSandboxView
	Desired    []FleetDesiredStateView
}

// GovernanceFreezeView and friends mirror the wire structs.
type GovernanceFreezeView struct {
	Reason         string
	ReasonKo       string
	AffectedRepos  []string
	AllowedActions []string
	NotAfterMs     int64
}

type GovernanceRecallView struct {
	Model       string
	Reason      string
	Replacement string
}

type GovernanceAckView struct {
	PolicyEpochID string
	SummaryKo     string
	Blocking      bool
}

type GovernanceVersionView struct {
	MinVersion string
	Ring       string
}

type GovernanceStandardView struct {
	RuleID        string
	BlockPattern  string
	Description   string
	DescriptionKo string
}

type GovernanceToolView struct {
	ToolID string
	Status string
}

type GovernanceSandboxView struct {
	RepositoryID string
	Mode         string
	RiskClass    string
}

type FleetDesiredStateView struct {
	Action     string
	Parameters string
}

// BuildGovernanceState assembles the wire snapshot from a view.
func BuildGovernanceState(v GovernanceStateView) *wireGovernanceState {
	snap := &wireGovernanceState{
		Version: 1, OrgID: v.OrgID, RepoID: v.RepoID, ModelID: v.ModelID,
	}
	if v.Freeze != nil {
		snap.Freeze = &wireGovernanceFreeze{
			Reason: v.Freeze.Reason, ReasonKo: v.Freeze.ReasonKo,
			AffectedRepos: v.Freeze.AffectedRepos, AllowedActions: v.Freeze.AllowedActions,
			NotAfterMs: v.Freeze.NotAfterMs,
		}
	}
	for _, r := range v.Recalls {
		snap.Recalls = append(snap.Recalls, wireGovernanceRecall(r))
	}
	for _, a := range v.Acks {
		snap.Acks = append(snap.Acks, wireGovernanceAck(a))
	}
	if v.VersionReq != nil {
		snap.VersionReq = &wireGovernanceVersion{MinVersion: v.VersionReq.MinVersion, Ring: v.VersionReq.Ring}
	}
	for _, s := range v.Standards {
		snap.Standards = append(snap.Standards, wireGovernanceStandard(s))
	}
	for _, t := range v.Tools {
		snap.Tools = append(snap.Tools, wireGovernanceTool(t))
	}
	for _, sb := range v.Sandboxes {
		snap.Sandboxes = append(snap.Sandboxes, wireGovernanceSandbox(sb))
	}
	for _, desired := range v.Desired {
		snap.Desired = append(snap.Desired, wireFleetDesiredState(desired))
	}
	return snap
}
