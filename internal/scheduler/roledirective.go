package scheduler

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// roledirective.go implements the WS2 role-change path (PAT-1445
// criterion 6): workers advertise verified co-located/prefill/decode
// roles on signed cards, and role changes happen only through signed
// desired configuration. The actuator turns the PD controller's
// engagement state into per-worker role directives, always keeping the
// co-located floor and never splitting engines that do not support
// disaggregation (SGLang upstream) — unsupported changes are rejected
// by construction.

// RoleDirective is a signed desired-role change for one worker.
type RoleDirective struct {
	WorkerID     string `json:"worker_id"`
	Model        string `json:"model"`
	Role         string `json:"role"` // aggregated | prefill | decode
	Reason       string `json:"reason"`
	SignatureHex string `json:"signature_hex,omitempty"`
}

// Sign binds the directive with the scheduler's evidence key.
func (d *RoleDirective) Sign(priv ed25519.PrivateKey) error {
	body := RoleDirective{WorkerID: d.WorkerID, Model: d.Model, Role: d.Role, Reason: d.Reason}
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	d.SignatureHex = hex.EncodeToString(ed25519.Sign(priv, raw))
	return nil
}

// Verify checks the directive signature.
func (d *RoleDirective) Verify(pub ed25519.PublicKey) bool {
	if d.SignatureHex == "" {
		return false
	}
	sig, err := hex.DecodeString(d.SignatureHex)
	if err != nil {
		return false
	}
	body := RoleDirective{WorkerID: d.WorkerID, Model: d.Model, Role: d.Role, Reason: d.Reason}
	raw, _ := json.Marshal(body)
	return ed25519.Verify(pub, raw, sig)
}

// RoleActuator converts PD engagement state into signed desired-role
// changes (the actuation itself belongs to S7's lifecycle executor).
type RoleActuator struct {
	pd *PDController
}

// NewRoleActuator builds the actuator over the PD controller.
func NewRoleActuator(pd *PDController) *RoleActuator {
	return &RoleActuator{pd: pd}
}

// Plan returns the desired role changes for one model's fleet. When the
// model is engaged, aggregated workers above the co-located floor split
// into prefill/decode pairs; when released, split workers revert to
// aggregated. SGLang workers are never split (unsupported upstream) and
// the co-located floor is always retained.
func (a *RoleActuator) Plan(model string, workers []FleetWorker) []RoleDirective {
	if a.pd == nil {
		return nil
	}
	floor := a.pd.MinColocated()
	var out []RoleDirective
	if !a.pd.Engaged(model) {
		// Released: every split-role worker reverts to aggregated.
		for _, w := range workers {
			if w.Entry.Card.ModelName != model {
				continue
			}
			if role := w.Entry.Card.EffectivePDRole(); role != PDRoleAggregated {
				out = append(out, RoleDirective{
					WorkerID: w.Entry.Card.WorkerID,
					Model:    model,
					Role:     PDRoleAggregated,
					Reason:   "disaggregation released: revert to co-located",
				})
			}
		}
		return out
	}

	// Engaged: split aggregated workers above the floor into P/D pairs.
	var aggregated []string
	for _, w := range workers {
		if w.Entry.Card.ModelName != model {
			continue
		}
		if w.Entry.Card.EngineKind == "sglang" {
			continue // unsupported role change: rejected by construction
		}
		if w.Entry.Quarantined || w.Entry.Lapsed || !w.Entry.Card.Servable() {
			continue
		}
		if w.Entry.Card.EffectivePDRole() == PDRoleAggregated {
			aggregated = append(aggregated, w.Entry.Card.WorkerID)
		}
	}
	splittable := len(aggregated) - floor
	for i := 0; i < splittable; i++ {
		role := PDRolePrefill
		if i%2 == 1 {
			role = PDRoleDecode
		}
		out = append(out, RoleDirective{
			WorkerID: aggregated[i],
			Model:    model,
			Role:     role,
			Reason:   fmt.Sprintf("disaggregation engaged: split %s (floor %d retained)", role, floor),
		})
	}
	return out
}
