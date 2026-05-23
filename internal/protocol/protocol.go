// Package protocol defines the control messages exchanged between the tunnl
// client and the tunnld relay during the connection handshake.
package protocol

import "encoding/json"

// MessageType identifies a control message.
type MessageType string

const (
	// TypeRegister is sent by the client to request a tunnel.
	TypeRegister MessageType = "register"
	// TypeRegistered is the relay's success reply.
	TypeRegistered MessageType = "registered"
	// TypeError is the relay's failure reply.
	TypeError MessageType = "error"
)

// Register asks the relay to open a tunnel.
type Register struct {
	Token  string `json:"token"`
	Target string `json:"target"` // informational, e.g. "http://localhost:3000"
}

// Registered tells the client which public URL was assigned.
type Registered struct {
	URL       string `json:"url"`
	Subdomain string `json:"subdomain"`
}

// Error reports why a request failed.
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Message is the single envelope sent over the control connection. Exactly one
// of the pointer fields is set, matching Type.
type Message struct {
	Type       MessageType `json:"type"`
	Register   *Register   `json:"register,omitempty"`
	Registered *Registered `json:"registered,omitempty"`
	Error      *Error      `json:"error,omitempty"`
}

// Encode marshals a Message to its wire form.
func Encode(m Message) ([]byte, error) {
	return json.Marshal(m)
}

// Decode parses a Message from its wire form.
func Decode(data []byte) (Message, error) {
	var m Message
	err := json.Unmarshal(data, &m)
	return m, err
}
