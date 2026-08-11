package signature

import "testing"

func TestBuildSignatureStringEmpty(t *testing.T) {
	got := BuildSignatureString(map[string]any{})
	if got != ",,,,,," {
		t.Fatalf("got %q", got)
	}
}

func TestBuildSignatureStringTimeZero(t *testing.T) {
	got := BuildSignatureString(map[string]any{"time": 0})
	if got != ",,,,0,," {
		t.Fatalf("got %q", got)
	}
}

func TestVerifyHMAC(t *testing.T) {
	msg := map[string]any{
		"robotId": "r1", "robotName": "n1",
		"operatorOpenid": "o1", "operatorName": "op",
		"time": 123, "msgId": "m1", "content": "hi",
	}
	plain := BuildSignatureString(msg)
	sig := ComputeHMACSHA1(plain, "secret")
	ok, errMsg := VerifySignature(msg, sig, "secret")
	if !ok || errMsg != "" {
		t.Fatalf("verify failed: %v %s", ok, errMsg)
	}
	ok, _ = VerifySignature(msg, "bad", "secret")
	if ok {
		t.Fatal("expected fail")
	}
	ok, _ = VerifySignature(msg, "", "")
	if !ok {
		t.Fatal("empty secret should skip")
	}
}
