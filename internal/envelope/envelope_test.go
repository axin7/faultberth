package envelope

import (
	"bytes"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	input := "{\"event_id\":\"123\",\"extra\":true}\n" +
		"{\"type\":\"event\",\"length\":5}\na\nbcd\n" +
		"{\"type\":\"future\"}\nunknown\n"
	got, err := Parse(strings.NewReader(input), 1024)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Items) != 2 || string(got.Items[0].Payload) != "a\nbcd" ||
		got.Items[1].Type != "future" || string(got.Items[1].Payload) != "unknown" {
		t.Fatalf("unexpected result: %#v", got)
	}
	if !bytes.Contains(got.Headers, []byte(`"extra":true`)) {
		t.Fatal("unknown envelope header was lost")
	}
}

func TestMalformed(t *testing.T) {
	cases := []string{
		"", "null\n", "{}\n[]\n", "{}\n{\"length\":1}\nx",
		"{}\n{\"type\":\"event\",\"length\":-1}\nx",
		"{}\n{\"type\":\"event\",\"length\":2}\nx",
		"{}\n{\"type\":\"event\",\"length\":1}\nxy",
	}
	for _, input := range cases {
		if _, err := Parse(strings.NewReader(input), 1024); err == nil {
			t.Errorf("accepted malformed envelope %q", input)
		}
	}
	if _, err := Parse(strings.NewReader("{}\n"), 2); err == nil {
		t.Fatal("accepted oversized envelope")
	}
}

func TestEmptyAndEOF(t *testing.T) {
	for _, input := range []string{"{}", "{}\n", "{}\n{\"type\":\"x\"}\n", "{}\n{\"type\":\"x\",\"length\":0}\n"} {
		if _, err := Parse(strings.NewReader(input), 1024); err != nil {
			t.Errorf("Parse(%q): %v", input, err)
		}
	}
}
