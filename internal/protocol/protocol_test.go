package protocol

import "testing"

func TestEncodeDecodeRegister(t *testing.T) {
	in := Message{Type: TypeRegister, Register: &Register{Token: "secret", Target: "http://localhost:3000"}}
	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Type != TypeRegister {
		t.Fatalf("type = %q, want %q", out.Type, TypeRegister)
	}
	if out.Register == nil || out.Register.Token != "secret" || out.Register.Target != "http://localhost:3000" {
		t.Fatalf("register = %+v, want token=secret target=http://localhost:3000", out.Register)
	}
}

func TestEncodeDecodeRegistered(t *testing.T) {
	in := Message{Type: TypeRegistered, Registered: &Registered{URL: "https://happy-fox-0001.example.com", Subdomain: "happy-fox-0001"}}
	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Registered == nil || out.Registered.Subdomain != "happy-fox-0001" {
		t.Fatalf("registered = %+v", out.Registered)
	}
}

func TestEncodeDecodeError(t *testing.T) {
	in := Message{Type: TypeError, Error: &Error{Code: "unauthorized", Message: "invalid token"}}
	data, err := Encode(in)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	out, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if out.Error == nil || out.Error.Code != "unauthorized" {
		t.Fatalf("error = %+v", out.Error)
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	if _, err := Decode([]byte("not json")); err == nil {
		t.Fatal("expected error decoding invalid JSON")
	}
}
