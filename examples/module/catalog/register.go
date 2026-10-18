package catalog

import (
	"context"

	"github.com/danielgtaylor/huma/v2"

	"github.com/wisalgroup/labena/internal/platform/api/response"
	"github.com/wisalgroup/labena/internal/platform/dependencies"
)

type GreetingRequest struct {
	Name string `path:"name" minLength:"1"`
}
type GreetingResponse struct {
	Message string `json:"message"`
}

// Register installs the greeting route without opening resources.
func Register(_ dependencies.Resources, targets dependencies.Targets) error {
	if targets.HTTP == nil {
		return nil
	}
	huma.Register(targets.HTTP, huma.Operation{Method: "GET", Path: "/catalog/hello/{name}", Summary: "Greet a visitor"},
		func(ctx context.Context, input *GreetingRequest) (*response.JSON[GreetingResponse], error) {
			return response.OK(GreetingResponse{Message: "Hello, " + input.Name}), nil
		})
	return nil
}
