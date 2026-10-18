package pagination

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPaginationBounds(t *testing.T) {
	for _, p := range []Pagination{{}, {-1, -1}, {2, 101}, {int(^uint(0) >> 1), 100}, {int(^uint(0) >> 1), 1}} {
		normalized := NewPagination(p.Page, p.PerPage)
		if normalized.Page < 1 || normalized.PerPage < 1 || normalized.PerPage > 100 || p.Offset() < 0 {
			t.Fatal(normalized)
		}
	}
	if NewPagination(3, 20).Offset() != 40 {
		t.Fatal("incorrect offset")
	}
	for _, tc := range []struct{ total, pages int }{{0, 0}, {20, 1}, {21, 2}, {-1, 0}} {
		page := NewPage[string](nil, Pagination{}, tc.total)
		data, err := json.Marshal(page)
		if err != nil || page.TotalPages != tc.pages || page.Page != 1 || page.PerPage != 20 || !strings.Contains(string(data), `"items":[]`) {
			t.Fatal(page, err)
		}
	}
	max := int(^uint(0) >> 1)
	if NewPage[string](nil, NewPagination(1, 100), max).TotalPages != 1+(max-1)/100 {
		t.Fatal("total pages overflow")
	}
}
