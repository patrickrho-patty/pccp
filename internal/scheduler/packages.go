package scheduler

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// packages.go implements PAT-1445 B3.2: the model-package identity source
// the gateway uses to stamp requests with their cache compatibility
// identity. The source is offline-capable (a local view, never a CP call
// on the request path); an unknown model yields no identity and the
// request stays on the legacy routing path (conservative fallback).

// PackageIdentity is the scheduler-side view of a signed model package
// (PAT-1444 ModelPackage: name/version + tokenizer/template digests).
type PackageIdentity struct {
	ModelPackage string `json:"model_package"`
	TokenizerID  string `json:"tokenizer_id"`
	TemplateID   string `json:"template_id"`
	AdapterID    string `json:"adapter_id,omitempty"`
	PolicyEpoch  string `json:"policy_epoch,omitempty"`
}

// CacheIdentity converts the package identity to the directory key.
func (p PackageIdentity) CacheIdentity() CacheIdentity {
	return CacheIdentity{
		ModelPackage: p.ModelPackage,
		TokenizerID:  p.TokenizerID,
		TemplateID:   p.TemplateID,
		AdapterID:    p.AdapterID,
		PolicyEpoch:  p.PolicyEpoch,
	}
}

// PackageSource resolves a served model to its signed package identity.
type PackageSource interface {
	PackageFor(model string) (PackageIdentity, bool)
}

// FilePackageSource is the file-backed source for local/sovereign
// deployments (mirrors the FilePolicy pattern): a JSON map of model name
// to package identity, loaded at startup and optionally reloaded by the
// operator. Safe for concurrent use.
type FilePackageSource struct {
	mu       sync.RWMutex
	packages map[string]PackageIdentity
}

// LoadPackageFile loads package identities from a JSON object mapping
// model names to PackageIdentity.
func LoadPackageFile(path string) (*FilePackageSource, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("scheduler: read package file: %w", err)
	}
	var pkgs map[string]PackageIdentity
	if err := json.Unmarshal(data, &pkgs); err != nil {
		return nil, fmt.Errorf("scheduler: decode package file: %w", err)
	}
	return &FilePackageSource{packages: pkgs}, nil
}

// NewFilePackageSource builds a source from an in-memory map (tests,
// composition roots that read the PAT-1444 registry themselves).
func NewFilePackageSource(pkgs map[string]PackageIdentity) *FilePackageSource {
	if pkgs == nil {
		pkgs = make(map[string]PackageIdentity)
	}
	return &FilePackageSource{packages: pkgs}
}

// PackageFor resolves a model to its package identity (ok=false unknown).
func (f *FilePackageSource) PackageFor(model string) (PackageIdentity, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	p, ok := f.packages[model]
	return p, ok
}
