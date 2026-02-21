package prompt

import "testing"

func TestParseFrontmatterValid(t *testing.T) {
	t.Parallel()
	content := "---\nname: skill-one\ndescription: description body\n---\nfirst line\n"

	fields, body := ParseFrontmatter(content)
	if len(fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(fields))
	}
	if fields["name"] != "skill-one" {
		t.Fatalf("unexpected name: %q", fields["name"])
	}
	if fields["description"] != "description body" {
		t.Fatalf("unexpected description: %q", fields["description"])
	}
	if body != "first line\n" {
		t.Fatalf("unexpected body: %q", body)
	}
}

func TestParseFrontmatterMissing(t *testing.T) {
	t.Parallel()
	content := "no frontmatter\nline two\n"

	fields, body := ParseFrontmatter(content)
	if len(fields) != 0 {
		t.Fatalf("expected empty fields, got %d", len(fields))
	}
	if body != content {
		t.Fatalf("expected full body, got %q", body)
	}
}

func TestParseFrontmatterMalformed(t *testing.T) {
	t.Parallel()
	content := "---\nname: missing-close\ndescription: nope"

	fields, body := ParseFrontmatter(content)
	if len(fields) != 0 {
		t.Fatalf("expected empty fields, got %d", len(fields))
	}
	if body != content {
		t.Fatalf("expected full body, got %q", body)
	}
}
