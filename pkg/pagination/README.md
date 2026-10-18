# Pagination

Transport-independent page/size normalization and result metadata. HTTP query
validation belongs to the API adapter; this package has no HTTP or application
dependencies.

```go
page := pagination.NewPagination(2, 20)
offset := page.Offset() // 20
result := pagination.NewPage(items, page, total)
```

Defaults are page 1 and 20 items. Page size is capped at 100, and page numbers
are capped to prevent integer overflow. Negative totals become zero. Empty items
serialize as an array, not null.

The result retains `items`, `page`, `per_page`, `total`, and `total_pages`.
Use stable ordering in repository queries. The result borrows the supplied item
slice; callers own its mutation and synchronization.
