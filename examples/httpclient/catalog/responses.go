package catalog

// ProductResponse is the provider's JSON payload; status and headers remain on
// httpclient.Response. Both the get and create operations return this shape.
type ProductResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
