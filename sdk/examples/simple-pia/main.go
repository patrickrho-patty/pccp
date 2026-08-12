// Example: Building a PIA for a custom serving engine using the PIA SDK.
//
// This demonstrates how anyone can create their own Patty Inference Agent
// that speaks PAPER and bridges to any model serving infrastructure.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"piapi"
)

// MyEngineAdapter is an example adapter for a hypothetical serving engine.
// In real use, this would call vLLM, SGLang, TGI, or any model server.
type MyEngineAdapter struct {
	modelID string
}

func (a *MyEngineAdapter) Complete(ctx context.Context, req *piapi.Request) (*piapi.Response, error) {
	// Example: echo the last user message
	var lastUserMsg string
	for _, m := range req.Messages {
		if m.Role == "user" {
			lastUserMsg = m.Content
		}
	}

	resp := fmt.Sprintf("Model %s received: %s", a.modelID, lastUserMsg)

	return &piapi.Response{
		Content:      resp,
		FinishReason: "COMPLETED",
		Usage: piapi.Usage{
			InputTokens:  len(lastUserMsg) / 4,
			OutputTokens: len(resp) / 4,
			TotalTokens:  (len(lastUserMsg) + len(resp)) / 4,
		},
	}, nil
}

func (a *MyEngineAdapter) HealthCheck(ctx context.Context) error {
	return nil // always healthy in this example
}

func (a *MyEngineAdapter) ModelID() string {
	return a.modelID
}

func main() {
	modelID := os.Getenv("MODEL_ID")
	if modelID == "" {
		modelID = "example-model"
	}
	addr := os.Getenv("PIA_ADDR")
	if addr == "" {
		addr = ":9444"
	}

	adapter := &MyEngineAdapter{modelID: modelID}
	pia := piapi.New("my-pia-01", adapter)

	log.Printf("Starting example PIA for model %s on %s", modelID, addr)
	if err := pia.Listen(addr); err != nil {
		log.Fatal(err)
	}
}
