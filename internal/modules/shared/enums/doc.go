// Package enums defines shared application enum types, grouped by business concept.
// String-backed enums already encode/decode as JSON strings and work with Bun and
// database/sql string conversion; no custom marshaler, Scanner, or Valuer is needed.
// Decoding preserves unknown strings. Call Valid at application boundaries before
// accepting them; entities translate invalid membership to their own error sentinels.
// Nullable database fields should use pointers rather than treating NULL as an enum.
package enums
