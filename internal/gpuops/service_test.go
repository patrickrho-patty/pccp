package gpuops

import (
	"path/filepath"
	"testing"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDB(t *testing.T) *gorm.DB {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	db.AutoMigrate(append(models.AllModels(), &identity.AdminCredentials{})...)
	return db
}

func TestEndpointMetricsUpdate(t *testing.T) {
	svc := New(setupDB(t))
	svc.UpdateEndpointMetrics("ep-1", EndpointMetrics{
		PIAVersion:     "1.0.0",
		ServingEngine:  "vllm",
		ActiveRequests: 5,
		TTFTMs:         120.5,
		DrainState:     "active",
	})

	m := svc.GetEndpointMetrics("ep-1")
	if m == nil {
		t.Fatal("expected metrics")
	}
	if m.TTFTMs != 120.5 {
		t.Fatalf("expected 120.5, got %f", m.TTFTMs)
	}
}

func TestGPUMetricsUpdate(t *testing.T) {
	svc := New(setupDB(t))
	svc.UpdateGPUMetrics("gpu-0", GPUMetrics{
		GPUModel:      "NVIDIA H100",
		Utilization:   75.5,
		VRAMTotalGB:   80,
		VRAMUsedGB:    60,
		TemperatureC:  72.0,
		ECCHealth:     "healthy",
		MaintenanceState: "active",
	})

	gpus := svc.GetAllGPUMetrics()
	if len(gpus) != 1 {
		t.Fatalf("expected 1 GPU, got %d", len(gpus))
	}
	if gpus[0].GPUModel != "NVIDIA H100" {
		t.Fatal("expected H100")
	}
}

func TestRoutingDecision(t *testing.T) {
	db := setupDB(t)
	svc := New(db)

	// Create model package and endpoint
	pkg := models.ModelPackage{PackageID: "pmp_route_test", ModelID: "route-model", Name: "Route Test", Version: "1.0", State: "published"}
	db.Create(&pkg)

	org := models.Organization{Name: "Route Test", Slug: "route-test", Status: "active"}
	db.Create(&org)

	ep := models.InferenceEndpoint{
		OrganizationID: org.ID, EndpointID: "ep-route-1",
		ModelPackageID: "pmp_route_test", ServingEngine: "vllm",
		Status: "active", PublicKey: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	}
	db.Create(&ep)

	decision, err := svc.RouteRequest(org.ID, "pmp_route_test", "")
	if err != nil {
		t.Fatal(err)
	}
	if decision.EndpointID != "ep-route-1" {
		t.Fatalf("expected ep-route-1, got %s", decision.EndpointID)
	}
}

func TestSetEndpointDrainState(t *testing.T) {
	svc := New(setupDB(t))
	svc.UpdateEndpointMetrics("ep-drain", EndpointMetrics{DrainState: "active"})
	err := svc.SetEndpointDrainState("ep-drain", "draining")
	if err != nil {
		t.Fatal(err)
	}
	m := svc.GetEndpointMetrics("ep-drain")
	if m.DrainState != "draining" {
		t.Fatal("expected draining")
	}
}
