package deepreview

import "testing"

func TestRenderTemplateMissingVariablesFailFast(t *testing.T) {
	_, err := RenderTemplate("hello {{NAME}} {{OTHER}}", map[string]string{"NAME": "x"})
	if err == nil {
		t.Fatal("expected missing variable error")
	}
}

func TestRenderTemplateUnresolvedVariablesFailFast(t *testing.T) {
	_, err := RenderTemplate("hello {{NAME}}", map[string]string{"NAME": "{{LEFT}}"})
	if err == nil {
		t.Fatal("expected unresolved variable error")
	}
}

func TestRenderTemplateSuccess(t *testing.T) {
	rendered, err := RenderTemplate("a={{A}} b={{B}}", map[string]string{"A": "1", "B": "2"})
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	if rendered != "a=1 b=2" {
		t.Fatalf("unexpected render output: %s", rendered)
	}
}
