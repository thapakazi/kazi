package store

import "testing"

func TestValidateManifest(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		ok   bool
	}{
		{"valid compose", "apiVersion: kazi.dev/v1alpha1\nkind: Stack\nmetadata:\n  name: blog\nspec:\n  source:\n    compose: /x/compose.yaml\n", true},
		{"valid template", "apiVersion: kazi.dev/v1alpha1\nkind: Stack\nmetadata:\n  name: pg\nspec:\n  source:\n    template: postgres\n", true},
		{"bad apiVersion", "apiVersion: v1\nkind: Stack\nmetadata:\n  name: blog\nspec:\n  source:\n    compose: /x\n", false},
		{"bad kind", "apiVersion: kazi.dev/v1alpha1\nkind: Pod\nmetadata:\n  name: blog\nspec:\n  source:\n    compose: /x\n", false},
		{"bad name", "apiVersion: kazi.dev/v1alpha1\nkind: Stack\nmetadata:\n  name: Bad_Name\nspec:\n  source:\n    compose: /x\n", false},
		{"no source", "apiVersion: kazi.dev/v1alpha1\nkind: Stack\nmetadata:\n  name: blog\nspec: {}\n", false},
		{"unknown field", "apiVersion: kazi.dev/v1alpha1\nkind: Stack\nmetadata:\n  name: blog\nspec:\n  source:\n    compose: /x\nbogus: true\n", false},
		{"not yaml", "::: not yaml :::\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateManifest([]byte(tc.yaml))
			if tc.ok && err != nil {
				t.Fatalf("expected valid, got %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("expected invalid, got nil")
			}
		})
	}
}
