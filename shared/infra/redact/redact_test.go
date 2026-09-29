package redact

import (
	"strings"
	"testing"
)

func TestRedactURI_StripsUserinfo(t *testing.T) {
	got := RedactURI("mongodb://root:devpassword123@localhost:27017/?authSource=admin")
	if strings.Contains(got, "devpassword123") || strings.Contains(got, "root@") {
		t.Errorf("credentials leaked: %q", got)
	}
	if !strings.Contains(got, "localhost:27017") {
		t.Errorf("host lost: %q", got)
	}
	if !strings.Contains(got, "authSource=admin") {
		t.Errorf("non-sensitive param lost: %q", got)
	}
}

func TestRedactURI_RedactsCredentialQueryParams(t *testing.T) {
	got := RedactURI("mongodb://localhost:27017/db?password=s3cret&token=abc&replicaSet=rs0")
	if strings.Contains(got, "s3cret") || strings.Contains(got, "token=abc") {
		t.Errorf("credential params leaked: %q", got)
	}
	if !strings.Contains(got, "password=REDACTED") || !strings.Contains(got, "token=REDACTED") {
		t.Errorf("params not marked redacted: %q", got)
	}
	if !strings.Contains(got, "replicaSet=rs0") {
		t.Errorf("non-sensitive param lost: %q", got)
	}
}

func TestRedactURI_NoCredentialsPassthrough(t *testing.T) {
	in := "mongodb://localhost:27017/user_db"
	if got := RedactURI(in); got != in {
		t.Errorf("clean URI altered: %q -> %q", in, got)
	}
}

func TestRedactURI_UnparseableFailsClosed(t *testing.T) {
	got := RedactURI("http://user:pass@host:badport/x:y:z")
	if strings.Contains(got, "pass") {
		t.Errorf("unparseable URI leaked input: %q", got)
	}
}

func TestRedactURI_CaseInsensitiveKeys(t *testing.T) {
	got := RedactURI("https://example.com/x?API_KEY=sekret")
	if strings.Contains(got, "sekret") {
		t.Errorf("case variant leaked: %q", got)
	}
}

func TestPreviewToken_Truncates(t *testing.T) {
	if got := PreviewToken("fcm-device-token-abcdef123456"); got != "fcm-device" {
		t.Errorf("unexpected preview: %q", got)
	}
	if got := PreviewToken("short"); got != "short" {
		t.Errorf("short token altered: %q", got)
	}
}
