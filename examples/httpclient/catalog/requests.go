package catalog

import "github.com/wisalgroup/labena/pkg/httpclient"

// GetProductRequest fetches a product, optionally selecting response fields.
type GetProductRequest struct {
	httpclient.Request `method:"GET" path:"products/{id}"`
	ID                 string   `path:"id"`
	Fields             []string `query:"fields,omitempty"`
}

// CreateProductRequest sends only Body as JSON.
type CreateProductRequest struct {
	httpclient.Request `method:"POST" path:"products"`
	Body               CreateProductBody `body:"json"`
}

type CreateProductBody struct {
	Name string `json:"name"`
}
