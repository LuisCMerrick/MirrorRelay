package proxy

import (
	"strings"
	"testing"
)

func TestBearerChallengeParserPreservesParametersAndQuotedComma(t *testing.T) {
	challenge, err := parseBearerChallenge(`Bearer scope="repository:a/b:pull",realm="https://auth.example/token?a=b,c=d",service="registry.example",vendor="x"`)
	if err != nil {
		t.Fatal(err)
	}
	if realm, ok := challenge.get("realm"); !ok || realm != "https://auth.example/token?a=b,c=d" {
		t.Fatalf("realm = %q, %v", realm, ok)
	}
	challenge.set("realm", "https://mirror.example/_mirror_auth/7/token")
	encoded := challenge.String()
	for _, want := range []string{`scope="repository:a/b:pull"`, `service="registry.example"`, `vendor="x"`, `realm="https://mirror.example/_mirror_auth/7/token"`} {
		if !strings.Contains(encoded, want) {
			t.Fatalf("challenge lost %s: %s", want, encoded)
		}
	}
}

func TestBearerChallengeParserRejectsMalformedInput(t *testing.T) {
	for _, value := range []string{`Bearer realm="unterminated`, `Bearer realm`, `Bearer realm="ok",,scope="x"`} {
		if _, err := parseBearerChallenge(value); err == nil {
			t.Fatalf("malformed challenge accepted: %q", value)
		}
	}
}
