package gpuops

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/gorm"
)

// Service implements Model and GPU Operations (PRD §30).
// Provides enterprise operators visibility into model serving and GPU health
// while keeping the serving plane isolated from source and employee content.
type Service struct {
	db *gorm.DB
	mu sync.RWMutex
	// In-memory metrics (production would use a time-series store)
	endpointMetrics map[string]*EndpointMetrics
	gpuMetrics      map[string]*GPUMetrics
}

// New creates a new GPU operations service.
func New(db *gorm.DB) *Service {
	return &Service{
		db:              db,
		endpointMetrics: make(map[string]*EndpointMetrics),
		gpuMetrics:      make(map[string]*GPUMetrics),
	}
}

// EndpointMetrics holds runtime metrics for a model endpoint (PRD §30.2).
type EndpointMetrics struct {
	EndpointID       string  `json:"endpoint_id"`
	PIAVersion       string  `json:"pia_version"`
	HostIdentity     string  `json:"host_identity"`
	Cluster          string  `json:"cluster"`
	ServingEngine    string  `json:"serving_engine"`
	ServingVersion   string  `json:"serving_version"`
	LoadedModel      string  `json:"loaded_model"`
	AttestationAge   string  `json:"attestation_age"`
	AssuranceLevel   string  `json:"assurance_level"`
	LeaseExpiry      string  `json:"lease_expiry"`
	ActiveRequests   int     `json:"active_requests"`
	QueuedRequests   int     `json:"queued_requests"`
	TTFTMs           float64 `json:"ttft_ms"` // Time To First Token
	DecodeLatencyMs  float64 `json:"decode_latency_ms"`
	KVCacheUsage     float64 `json:"kv_cache_usage"`
	FailureCount     int64   `json:"failure_count"`
	RestartCount     int     `json:"restart_count"`
	RoutingWeight    int     `json:"routing_weight"`
	DrainState       string  `json:"drain_state"` // active, draining, canary
	UpdatedAt        string  `json:"updated_at"`
}

// GPUMetrics holds GPU health metrics (PRD §30.3).
type GPUMetrics struct {
	GPUID            string  `json:"gpu_id"`
	GPUModel         string  `json:"gpu_model"` // e.g. "NVIDIA H100"
	SerialOrAttestID string  `json:"serial_or_attestation_id"`
	EndpointID       string  `json:"endpoint_id,omitempty"`
	Utilization      float64 `json:"utilization"` // 0-100
	VRAMTotalGB      float64 `json:"vram_total_gb"`
	VRAMUsedGB       float64 `json:"vram_used_gb"`
	TemperatureC     float64 `json:"temperature_c"`
	PowerWatts       float64 `json:"power_watts"`
	ECCHealth        string  `json:"ecc_health"` // healthy, degraded, critical
	MIGPartitioning  string  `json:"mig_partitioning,omitempty"`
	ModelReplicas    int     `json:"model_replicas"`
	QueueDepth       int     `json:"queue_depth"`
	MaintenanceState string  `json:"maintenance_state"` // active, maintenance, drained
	UpdatedAt        string  `json:"updated_at"`
}

// UpdateEndpointMetrics updates metrics for an endpoint.
func (s *Service) UpdateEndpointMetrics(endpointID string, metrics EndpointMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics.EndpointID = endpointID
	metrics.UpdatedAt = time.Now().Format(time.RFC3339)
	s.endpointMetrics[endpointID] = &metrics
}

// GetEndpointMetrics returns metrics for an endpoint.
func (s *Service) GetEndpointMetrics(endpointID string) *EndpointMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.endpointMetrics[endpointID]
}

// GetAllEndpointMetrics returns all endpoint metrics.
func (s *Service) GetAllEndpointMetrics() []EndpointMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]EndpointMetrics, 0, len(s.endpointMetrics))
	for _, m := range s.endpointMetrics {
		result = append(result, *m)
	}
	return result
}

// UpdateGPUMetrics updates GPU metrics.
func (s *Service) UpdateGPUMetrics(gpuID string, metrics GPUMetrics) {
	s.mu.Lock()
	defer s.mu.Unlock()
	metrics.GPUID = gpuID
	metrics.UpdatedAt = time.Now().Format(time.RFC3339)
	s.gpuMetrics[gpuID] = &metrics
}

// GetAllGPUMetrics returns all GPU metrics.
func (s *Service) GetAllGPUMetrics() []GPUMetrics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]GPUMetrics, 0, len(s.gpuMetrics))
	for _, m := range s.gpuMetrics {
		result = append(result, *m)
	}
	return result
}

// RoutingDecision determines where to route a model request (PRD §30.4).
type RoutingDecision struct {
	EndpointID    string `json:"endpoint_id"`
	Reason        string `json:"reason"`
	DataResidency string `json:"data_residency,omitempty"`
}

// RouteRequest selects the best endpoint for a model request (PRD §30.4).
func (s *Service) RouteRequest(orgID, modelPackageID string, dataResidency string) (*RoutingDecision, error) {
	// Find active endpoints for this model
	var endpoints []models.InferenceEndpoint
	s.db.Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
		orgID, modelPackageID).Find(&endpoints)

	if len(endpoints) == 0 {
		return nil, fmt.Errorf("gpuops: no active endpoints for model %s", modelPackageID)
	}

	// Select the endpoint with the best metrics (lowest TTFT)
	var best *models.InferenceEndpoint
	bestTTFT := 999999.0
	for i := range endpoints {
		ep := &endpoints[i]
		metrics := s.GetEndpointMetrics(ep.EndpointID)
		if metrics != nil && metrics.DrainState == "active" && metrics.TTFTMs < bestTTFT {
			bestTTFT = metrics.TTFTMs
			best = ep
		}
	}

	// Fall back to first available if no metrics
	if best == nil {
		best = &endpoints[0]
	}

	decision := &RoutingDecision{
		EndpointID: best.EndpointID,
		Reason:     "selected by routing policy (best TTFT or first available)",
	}
	if dataResidency != "" {
		decision.DataResidency = dataResidency
	}

	return decision, nil
}

// ModelOperationsReport returns model operations data (PRD §30.1).
type ModelOperationsReport struct {
	ModelPackageID    string  `json:"model_package_id"`
	LogicalName       string  `json:"logical_name"`
	ArtifactID        string  `json:"artifact_id"`
	ContextLimit      int     `json:"context_limit"`
	EndpointCount     int     `json:"endpoint_count"`
	CurrentTraffic    int     `json:"current_traffic"`
	AvgTTFTMs         float64 `json:"avg_ttft_ms"`
	AvgDecodeMs       float64 `json:"avg_decode_ms"`
	ErrorRate         float64 `json:"error_rate"`
	Status            string  `json:"status"`
}

// GetModelOperationsReport returns operations data for all models.
func (s *Service) GetModelOperationsReport(orgID string) ([]ModelOperationsReport, error) {
	var packages []models.ModelPackage
	s.db.Where("state = 'published'").Find(&packages)

	var reports []ModelOperationsReport
	for _, pkg := range packages {
		report := ModelOperationsReport{
			ModelPackageID: pkg.PackageID,
			LogicalName:    pkg.Name,
			ArtifactID:     pkg.ManifestDigest,
			ContextLimit:   pkg.ContextWindow,
			Status:         pkg.State,
		}

		// Count endpoints
		var count int64
		s.db.Model(&models.InferenceEndpoint{}).
			Where("organization_id = ? AND model_package_id = ? AND status = 'active'",
				orgID, pkg.PackageID).Count(&count)
		report.EndpointCount = int(count)

		// Aggregate metrics from endpoints
		var totalTTFT, totalDecode float64
		var metricCount int
		for _, m := range s.GetAllEndpointMetrics() {
			var ep models.InferenceEndpoint
			if s.db.Where("endpoint_id = ? AND model_package_id = ?", m.EndpointID, pkg.PackageID).First(&ep).Error == nil {
				totalTTFT += m.TTFTMs
				totalDecode += m.DecodeLatencyMs
				report.CurrentTraffic += m.ActiveRequests
				metricCount++
			}
		}
		if metricCount > 0 {
			report.AvgTTFTMs = totalTTFT / float64(metricCount)
			report.AvgDecodeMs = totalDecode / float64(metricCount)
		}

		reports = append(reports, report)
	}
	return reports, nil
}

// SetEndpointDrainState sets the drain/canary state for an endpoint.
func (s *Service) SetEndpointDrainState(endpointID, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m, ok := s.endpointMetrics[endpointID]; ok {
		m.DrainState = state
		return nil
	}
	return fmt.Errorf("gpuops: endpoint %s metrics not found", endpointID)
}

// Ensure json import used
var _ = json.Marshal
