package setup

import (
	"bytes"
	"strings"
	"testing"
)

func TestChooseOneUsesCurrentOnEnter(t *testing.T) {
	in := strings.NewReader("\n")
	var out bytes.Buffer
	u := newUI(in, &out)
	v, err := u.chooseOne("Mode", []string{"a", "b"}, "b")
	if err != nil {
		t.Fatalf("chooseOne: %v", err)
	}
	if v != "b" {
		t.Fatalf("value = %q", v)
	}
}

func TestChooseOneUsesFirstWhenNoCurrentAndEnter(t *testing.T) {
	in := strings.NewReader("\n")
	var out bytes.Buffer
	u := newUI(in, &out)
	v, err := u.chooseOne("Mode", []string{"a", "b"}, "")
	if err != nil {
		t.Fatalf("chooseOne: %v", err)
	}
	if v != "a" {
		t.Fatalf("value = %q", v)
	}
}
