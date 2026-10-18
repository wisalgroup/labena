package pagination

// Pagination holds the validated, normalised page/per_page values.
// Use NewPagination to construct it. Transport validation belongs to the API.
type Pagination struct {
	Page    int
	PerPage int
}

// NewPagination returns a validated Pagination.
// Defaults: page=1, per_page=20. Maximum per_page=100. Pages are capped
// only to prevent integer overflow. HTTP callers reject invalid values before
// construction; programmatic callers receive normalized values.
func NewPagination(page, perPage int) Pagination {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}

	// Keep SQL offsets representable even for extreme programmatic inputs.
	maxPage := (int(^uint(0)>>1)-1)/perPage + 1
	if page > maxPage {
		page = maxPage
	}
	return Pagination{
		Page:    page,
		PerPage: perPage,
	}
}

// Offset returns the SQL OFFSET value for the current page.
func (p Pagination) Offset() int {
	p = NewPagination(p.Page, p.PerPage)
	return (p.Page - 1) * p.PerPage
}

// Page is the standard paginated response envelope.
// Items is always a non-nil slice so JSON serialises as [] not null.
type Page[T any] struct {
	Items      []T `json:"items"`
	Page       int `json:"page"`
	PerPage    int `json:"per_page"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}

// NewPage constructs a Page[T] from a slice of items, a Pagination, and the
// total record count. TotalPages is calculated automatically.
func NewPage[T any](items []T, p Pagination, total int) *Page[T] {
	p = NewPagination(p.Page, p.PerPage)
	if total < 0 {
		total = 0
	}
	if items == nil {
		items = []T{}
	}
	totalPages := 0
	if p.PerPage > 0 && total > 0 {
		totalPages = 1 + (total-1)/p.PerPage
	}
	return &Page[T]{
		Items:      items,
		Page:       p.Page,
		PerPage:    p.PerPage,
		Total:      total,
		TotalPages: totalPages,
	}
}

func (p *Page[T]) MapItems[O any](fn func(T) O) *Page[O] {
	items := make([]O, len(p.Items))
	for i, item := range p.Items {
		items[i] = fn(item)
	}
	return NewPage(items, Pagination{Page: p.Page, PerPage: p.PerPage}, p.Total)
}
