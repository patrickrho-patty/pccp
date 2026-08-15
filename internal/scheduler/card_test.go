package scheduler

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
)

func testCard() WorkerCard {
	return WorkerCard{
		CardVersion:         1,
		WorkerID:            "wkr-test-001",
		EnrollmentID:        "enr-test-001",
		NodeID:              "node-1",
		Hostname:            "gpu-node-1",
		IP:                  "10.0.0.7",
		Region:              "ap-northeast-2",
		Zone:                "ap-northeast-2a",
		EngineKind:          "vllm",
		EngineVersion:       "0.9.0",
		EngineURL:           "http://127.0.0.1:8033/v1",
		ReachabilityMode:    "localhost",
		MeasuredGrade:       "localhost",
		ModelName:           "qwen3.6-27b",
		ModelVersion:        "fp8",
		Precision:           "fp8",
		ContextLength:       131072,
		MaxConcurrentSeqs:   64,
		Modalities:          []string{"text", "image"},
		TP:                  1,
		DP:                  1,
		EP:                  0,
		AcceleratorFamily:   "nvidia",
		GPUSKU:              "H100-SXM-80GB",
		GPUCount:            1,
		HBMGB:               80,
		Status:              "active",
		LastHeartbeatUnixMs: 0,
		LeaseExpiryUnixMs:   0,
	}
}

func TestCardSigningBytesStableAndDomainPrefixed(t *testing.T) {
	card := testCard()
	b1 := card.SigningBytes()
	b2 := card.SigningBytes()
	if !bytes.Equal(b1, b2) {
		t.Fatal("signing bytes are not deterministic")
	}
	if !bytes.HasPrefix(b1, []byte(CardDomain)) {
		t.Fatal("signing bytes missing domain prefix")
	}
}

func TestCardSignVerifyRoundtrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	card := testCard()
	if err := card.Sign(priv); err != nil {
		t.Fatalf("sign: %v", err)
	}
	if err := card.Verify(pub); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestCardVerifyRejectsTamperedField(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	card := testCard()
	if err := card.Sign(priv); err != nil {
		t.Fatal(err)
	}
	card.ModelName = "tampered-model"
	if err := card.Verify(pub); err == nil {
		t.Fatal("expected verification failure for tampered field")
	}
}

func TestCardVerifyRejectsWrongKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherPub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	card := testCard()
	if err := card.Sign(priv); err != nil {
		t.Fatal(err)
	}
	if err := card.Verify(otherPub); err == nil {
		t.Fatal("expected verification failure for wrong key")
	}
}

func TestCardVerifyRejectsUnsigned(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	card := testCard()
	if err := card.Verify(pub); err == nil {
		t.Fatal("expected verification failure for missing signature")
	}
}
