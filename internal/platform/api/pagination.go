package api

import (
	"github.com/danielgtaylor/huma/v2"
	"github.com/wisalgroup/labena/pkg/pagination"
	"net/url"
)

// PaginationInput binds and validates pagination once, including ambiguous queries.
type PaginationInput struct {
	Page    int `query:"page" default:"1" minimum:"1"`
	PerPage int `query:"per_page" default:"20" minimum:"1" maximum:"100"`
}

func (in *PaginationInput) Resolve(ctx huma.Context) []error {
	values, err := url.ParseQuery(ctx.URL().RawQuery)
	if err != nil {
		return []error{&huma.ErrorDetail{Location: "query", Message: "Invalid query string."}}
	}
	for _, name := range []string{"page", "per_page"} {
		if v, ok := values[name]; ok && (len(v) != 1 || v[0] == "") {
			return []error{&huma.ErrorDetail{Location: "query." + name, Message: "Provide one positive integer."}}
		}
	}
	if in.Page > 0 && in.PerPage > 0 && pagination.NewPagination(in.Page, in.PerPage).Page != in.Page {
		return []error{&huma.ErrorDetail{Location: "query.page", Message: "Page exceeds the supported range."}}
	}
	return nil
}
func (in PaginationInput) Pagination() pagination.Pagination {
	return pagination.NewPagination(in.Page, in.PerPage)
}
