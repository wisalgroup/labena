package database

import (
	"strings"
	"unicode/utf8"

	"github.com/DataDog/go-sqllexer"
	"github.com/uptrace/bun/dialect"
)

const (
	maxQueryInput = 64 << 10
	maxQueryText  = 8 << 10
)

// sanitizedQuery keeps query structure, never values or comments. This is for
// trace attributes, not execution or metric labels. Identifiers remain visible:
// applications must not put secrets in table, column or alias names.
//
// Omit text when lexical interpretation is uncertain. In particular, backslash
// quoting depends on connection settings which a Bun hook cannot safely infer.
// PostgreSQL hex literals are the exception: their restricted contents cannot
// escape the closing quote, and the entire literal is redacted.
// The span still records its operation, duration and status when text is omitted.
func sanitizedQuery(query string, driver dialect.Name) string {
	if len(query) > maxQueryInput || !utf8.ValidString(query) || strings.ContainsRune(query, '\x00') {
		return ""
	}
	var dbms sqllexer.DBMSType
	switch driver {
	case dialect.PG, dialect.SQLite:
		dbms = sqllexer.DBMSPostgres
	case dialect.MySQL:
		dbms = sqllexer.DBMSMySQL
	default:
		return ""
	}
	lexer := sqllexer.New(query, sqllexer.WithDBMS(dbms))
	var out strings.Builder
	space, ended, number := false, false, false
	for {
		token := lexer.Scan()
		value := token.Value
		if strings.ContainsRune(value, '\\') {
			// Bun formats PostgreSQL []byte values as '\x<hex>'. Permit only
			// that complete string token; all other escape forms remain unsafe.
			if driver != dialect.PG || token.Type != sqllexer.STRING || !postgresHexLiteral(value) {
				return ""
			}
		}
		switch token.Type {
		case sqllexer.EOF:
			return out.String()
		case sqllexer.SPACE, sqllexer.COMMENT:
			space = true
			continue
		case sqllexer.MULTILINE_COMMENT:
			// The lexer does not implement PostgreSQL's nested block comments.
			if strings.Contains(value[2:], "/*") {
				return ""
			}
			space = true
			continue
		case sqllexer.ERROR, sqllexer.UNKNOWN, sqllexer.INCOMPLETE_STRING:
			return ""
		case sqllexer.STRING, sqllexer.NUMBER, sqllexer.BOOLEAN, sqllexer.NULL,
			sqllexer.DOLLAR_QUOTED_STRING, sqllexer.DOLLAR_QUOTED_FUNCTION,
			sqllexer.POSITIONAL_PARAMETER, sqllexer.BIND_PARAMETER:
			value = "?"
		case sqllexer.QUOTED_IDENT:
			// SQLite accepts double-quoted string values as well as identifiers.
			if driver == dialect.SQLite {
				value = "?"
			}
		case sqllexer.IDENT, sqllexer.FUNCTION, sqllexer.KEYWORD, sqllexer.COMMAND,
			sqllexer.CTE_INDICATOR, sqllexer.ALIAS_INDICATOR:
			// Reject numeric forms the lexer splits (e.g. 0b101 or 1_000).
			if number && !space {
				return ""
			}
			// Keywords next to operators may be classified as identifiers.
			switch strings.ToUpper(value) {
			case "TRUE", "FALSE", "NULL":
				value = "?"
			}
		case sqllexer.OPERATOR, sqllexer.WILDCARD, sqllexer.PUNCTUATION, sqllexer.JSON_OP:
			// SQLite bracket identifiers are not understood by this lexer.
			if driver == dialect.SQLite && value == "[" {
				return ""
			}
		default:
			return ""
		}
		if ended {
			return "" // Multiple statements can contain non-query commands.
		}
		if out.Len() == 0 {
			switch strings.ToUpper(value) {
			case "SELECT", "INSERT", "UPDATE", "DELETE", "WITH", "REPLACE":
			default:
				return "" // Administrative SQL can contain unquoted secrets.
			}
		} else if space {
			out.WriteByte(' ')
		}
		out.WriteString(value)
		if out.Len() > maxQueryText {
			return "" // Never truncate raw SQL before sanitizing it.
		}
		space, ended, number = false, value == ";", token.Type == sqllexer.NUMBER
	}
}

func postgresHexLiteral(value string) bool {
	if len(value) < 4 || !strings.HasPrefix(value, `'\x`) || !strings.HasSuffix(value, `'`) {
		return false
	}
	hex := value[3 : len(value)-1]
	return len(hex)%2 == 0 && strings.Trim(hex, "0123456789abcdefABCDEF") == ""
}
