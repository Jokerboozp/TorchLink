package ports

import (
	"context"

	"iot-platform/internal/model"
)

// ProductStore keeps device templates (products) and their onboarding
// bundles.
type ProductStore interface {
	// Keep atomic onboarding in the repository contract so telemetry/cache
	// decorators forward it to durable storage rather than hiding the capability.
	SaveOnboarding(context.Context, model.OnboardingBundle) error
	SaveProduct(context.Context, model.Product) error
	GetProduct(context.Context, string, string) (model.Product, error)
	GetProductsByIDs(context.Context, string, []string) (map[string]model.Product, error)
	ListProducts(context.Context, string) ([]model.Product, error)
	// ListProductsPage(ctx, tenant, query, limit, offset): query matches the ID or name, case-insensitively.
	ListProductsPage(context.Context, string, string, int, int) ([]model.Product, int, error)
}
