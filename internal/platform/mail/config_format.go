package mail

import (
	"encoding/json"
	"fmt"
)

// String and GoString keep credentials out of ordinary diagnostic formatting.
func (c SMTPConfig) String() string {
	return fmt.Sprintf("mail.SMTPConfig{Host:%q Port:%d UseTLS:%t}", c.Host, c.Port, c.UseTLS)
}
func (c SMTPConfig) GoString() string { return c.String() }

// MarshalJSON redacts the password; normal JSON decoding remains supported.
func (c SMTPConfig) MarshalJSON() ([]byte, error) {
	type configJSON SMTPConfig
	copy := configJSON(c)
	copy.Password = ""
	return json.Marshal(copy)
}
