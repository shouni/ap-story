// Package adapters provides the adapters that connect go-comic-kit to external services,
// such as initialising the Gemini client and loading the character definitions.
package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/shouni/genai-kit/gemini"

	"github.com/shouni/ap-story/internal/config"
)

const (
	// defaultVertexLocationID is the default Vertex AI location.
	// It is fixed independently of the region of Cloud Tasks and the like (GCP_LOCATION_ID).
	defaultVertexLocationID = "global"
	// defaultVertexInitialDelay is the initial wait before a retry.
	defaultVertexInitialDelay = 60 * time.Second
)

// NewVertexAIAdapter initialises the GCP Vertex AI client.
// It is passed as go-comic-kit's workflow.Args.AIClient. In Vertex AI mode GCS URIs
// can be referenced directly (skipping File API uploads), which keeps image generation cheaper.
func NewVertexAIAdapter(ctx context.Context, gcp config.GCPConfig) (*gemini.Client, error) {
	if gcp.ProjectID == "" {
		return nil, fmt.Errorf("GCP_PROJECT_ID is not set")
	}

	clientConfig := gemini.Config{
		ProjectID:    gcp.ProjectID,
		LocationID:   defaultVertexLocationID,
		InitialDelay: defaultVertexInitialDelay,
	}

	aiClient, err := gemini.New(ctx, clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to initialise the Vertex AI client: %w", err)
	}

	return aiClient, nil
}
