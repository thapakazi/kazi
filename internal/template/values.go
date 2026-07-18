package template

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadValues reads values.yaml from dir. The reserved "description" key is
// extracted separately; all other keys are flattened into a string map.
// Nested maps have their keys joined with '_'. Scalar values are converted
// with fmt.Sprint.
func LoadValues(dir string) (desc string, vals map[string]string, err error) {
	data, err := os.ReadFile(filepath.Join(dir, "values.yaml"))
	if err != nil {
		if os.IsNotExist(err) {
			return "", map[string]string{}, nil
		}
		return "", nil, fmt.Errorf("reading values.yaml in %s: %w", dir, err)
	}
	return parseValuesYAML(data)
}

// parseValuesYAML parses raw YAML bytes into description + flattened map.
func parseValuesYAML(data []byte) (desc string, vals map[string]string, err error) {
	var raw map[string]interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return "", nil, fmt.Errorf("parsing values.yaml: %w", err)
	}

	vals = map[string]string{}
	flattenMap(raw, "", vals)

	desc = vals["description"]
	delete(vals, "description")
	return desc, vals, nil
}

// flattenMap recursively flattens a map[string]interface{} into dst,
// joining nested keys with '_'. Scalar values are formatted with fmt.Sprint.
func flattenMap(m map[string]interface{}, prefix string, dst map[string]string) {
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "_" + k
		}
		switch val := v.(type) {
		case map[string]interface{}:
			flattenMap(val, key, dst)
		case map[interface{}]interface{}:
			// yaml.v3 sometimes produces this type for old-style YAML
			converted := make(map[string]interface{}, len(val))
			for mk, mv := range val {
				converted[fmt.Sprint(mk)] = mv
			}
			flattenMap(converted, key, dst)
		default:
			dst[key] = fmt.Sprint(val)
		}
	}
}

// MergeValues returns a copy of base with each "k=v" in sets applied.
// Keys are normalized to lowercase. An entry without '=' returns an error.
func MergeValues(base map[string]string, sets []string) (map[string]string, error) {
	result := make(map[string]string, len(base))
	for k, v := range base {
		result[k] = v
	}

	for _, s := range sets {
		idx := strings.Index(s, "=")
		if idx < 0 {
			return nil, fmt.Errorf("invalid --set value %q: must be key=value", s)
		}
		k := strings.ToLower(s[:idx])
		v := s[idx+1:]
		result[k] = v
	}
	return result, nil
}

// FlattenEnv converts a vals map to "KEY=val" env entries with keys
// upper-cased and the slice sorted alphabetically.
func FlattenEnv(vals map[string]string) []string {
	out := make([]string, 0, len(vals))
	for k, v := range vals {
		out = append(out, strings.ToUpper(k)+"="+v)
	}
	sort.Strings(out)
	return out
}
