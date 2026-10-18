package database

import (
	"strings"
	"testing"

	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect"
	"github.com/uptrace/bun/dialect/pgdialect"
)

func TestPostgresBinaryInsertQueryText(t *testing.T) {
	// Verification challenges and sessions store binary digests. Exercise Bun's
	// actual PostgreSQL formatting without opening a database connection.
	db := bun.NewDB(nil, pgdialect.New())
	model := &struct {
		CodeDigest []byte
	}{CodeDigest: []byte("private-digest")}
	query := db.NewInsert().Model(model).ModelTableExpr("verification_challenges").String()
	want := `INSERT INTO verification_challenges ("code_digest") VALUES (?)`
	if got := sanitizedQuery(query, dialect.PG); got != want {
		t.Fatalf("binary INSERT structure missing: got %q, want %q", got, want)
	}
}

func TestSanitizedQuery(t *testing.T) {
	tests := []struct {
		name   string
		driver dialect.Name
		query  string
		want   string
	}{
		{"postgres select", dialect.PG, `SELECT "u"."email" FROM "users" AS "u" WHERE id = 123 AND email = 'private@example.com'`, `SELECT "u"."email" FROM "users" AS "u" WHERE id = ? AND email = ?`},
		{"insert", dialect.PG, `INSERT INTO users (email, age) VALUES ('private@example.com', 42) RETURNING id`, `INSERT INTO users (email, age) VALUES (?, ?) RETURNING id`},
		{"postgres hex bytes", dialect.PG, `INSERT INTO proofs (digest) VALUES ('\x1234aBcD')`, `INSERT INTO proofs (digest) VALUES (?)`},
		{"postgres empty bytes", dialect.PG, `INSERT INTO proofs (digest) VALUES ('\x')`, `INSERT INTO proofs (digest) VALUES (?)`},
		{"postgres malformed hex", dialect.PG, `SELECT '\x12private'`, ""},
		{"postgres hex with escaped quote", dialect.PG, `SELECT '\x12\'private'`, ""},
		{"postgres hex followed by unsafe literal", dialect.PG, `SELECT '\x12', 'private\'value'`, ""},
		{"mysql hex backslash remains ambiguous", dialect.MySQL, `SELECT '\x1234'`, ""},
		{"postgres backslash identifier", dialect.PG, `SELECT "\x1234"`, ""},
		{"update", dialect.PG, `UPDATE users SET email='private@example.com' WHERE id=123`, `UPDATE users SET email=? WHERE id=?`},
		{"delete", dialect.PG, `DELETE FROM users WHERE id IN (123, 456)`, `DELETE FROM users WHERE id IN (?, ?)`},
		{"cte", dialect.PG, `WITH u AS (SELECT id FROM users WHERE email='private@example.com') SELECT * FROM u`, `WITH u AS (SELECT id FROM users WHERE email=?) SELECT * FROM u`},
		{"dollar quotes", dialect.PG, `SELECT $$private 'value'$$, $tag$private $other$value$tag$, $1`, `SELECT ?, ?, ?`},
		{"doubled quotes", dialect.PG, `SELECT 'private''value', 'مرحبا'`, `SELECT ?, ?`},
		{"literal kinds", dialect.PG, `SELECT -42, +9.5e-3, .25, TRUE, FALSE, NULL, DATE '2026-01-01', X'abcdef', B'1010'`, `SELECT ?, ?, .?, ?, ?, ?, DATE ?, X?, B?`},
		{"keyword operators", dialect.PG, `SELECT TRUE::boolean, FALSE=FALSE, NULL::text`, `SELECT ?::boolean, ?=?, ?::text`},
		{"numeric alias", dialect.PG, `SELECT 123 AS total`, `SELECT ? AS total`},
		{"json", dialect.PG, `SELECT payload->>'private-key' FROM records WHERE payload @> '{"email":"private@example.com"}'::jsonb`, `SELECT payload->>? FROM records WHERE payload @> ?::jsonb`},
		{"comments", dialect.PG, " /* private */ SELECT/*private*/'private' --private\n FROM users; -- private", `SELECT ? FROM users;`},
		{"mysql", dialect.MySQL, "SELECT `email` FROM `users` WHERE email=\"private@example.com\" AND id=0xabcdef # private", "SELECT `email` FROM `users` WHERE email=? AND id=?"},
		{"mysql parameters", dialect.MySQL, `SELECT email FROM users WHERE email=? AND id=@user_id`, `SELECT email FROM users WHERE email=? AND id=?`},
		{"sqlite quoted values", dialect.SQLite, `SELECT "private-value", 'private', 123 FROM users`, `SELECT ?, ?, ? FROM users`},
		{"nested comments", dialect.PG, `SELECT 1 /* outer /* nested */ private-value */`, ""},
		{"unterminated comment", dialect.PG, `SELECT 1 /* private`, ""},
		{"unterminated string", dialect.PG, `SELECT 'private`, ""},
		{"unterminated dollar quote", dialect.PG, `SELECT $tag$private`, ""},
		{"unterminated identifier", dialect.PG, `SELECT "private`, ""},
		{"ambiguous postgres escapes", dialect.PG, `SELECT E'private\'value'`, ""},
		{"ambiguous mysql escapes", dialect.MySQL, `SELECT 'private\'value'`, ""},
		{"binary number", dialect.PG, `SELECT 0b10101`, ""},
		{"octal number", dialect.PG, `SELECT 0o12345`, ""},
		{"numeric separators", dialect.PG, `SELECT 1_234_567`, ""},
		{"sqlite brackets", dialect.SQLite, `SELECT [private]`, ""},
		{"sqlite backticks", dialect.SQLite, "SELECT `private`", ""},
		{"administration", dialect.PG, `SET password = private`, ""},
		{"multiple statements", dialect.PG, `SELECT 1; SET password = private`, ""},
		{"unsupported dialect", dialect.Invalid, `SELECT 'private'`, ""},
		{"invalid utf8", dialect.PG, "SELECT '\xff'", ""},
		{"nul", dialect.PG, "SELECT 1\x00 private", ""},
		{"empty", dialect.PG, "", ""},
		{"only comments", dialect.PG, "/* private */", ""},
		{"oversized input", dialect.PG, "SELECT '" + strings.Repeat("x", maxQueryInput) + "'", ""},
		{"oversized output", dialect.PG, "SELECT " + strings.Repeat("column_name, ", maxQueryText/8) + "id FROM users", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sanitizedQuery(tt.query, tt.driver); got != tt.want {
				t.Errorf("sanitized query = %q, want %q", got, tt.want)
			}
		})
	}
}

func FuzzSanitizedQuery(f *testing.F) {
	for _, query := range []string{"SELECT 'private'", "SELECT 0b101", "SELECT $x$private$x$", "SELECT 1 /* nested /* x */ y */", `SELECT "value" FROM "users"`} {
		f.Add(query)
	}
	f.Fuzz(func(t *testing.T, query string) {
		for _, driver := range []dialect.Name{dialect.PG, dialect.MySQL, dialect.SQLite} {
			if len(sanitizedQuery(query, driver)) > maxQueryText {
				t.Fatal("query text exceeded its limit")
			}
			literal := "SELECT '" + strings.ReplaceAll(query, "'", "''") + "'"
			if got := sanitizedQuery(literal, driver); got != "" && got != "SELECT ?" {
				t.Fatal("literal contents escaped redaction")
			}
		}
	})
}
