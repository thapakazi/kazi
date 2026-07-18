package template

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/runtime"
)

var (
	// ErrInvalidTemplate is returned when the edited compose.yml fails
	// `compose config --quiet` validation.
	ErrInvalidTemplate = errors.New("template failed compose validation")

	// ErrAborted is returned when the editor is closed with an empty file.
	ErrAborted = errors.New("template creation aborted")
)

// OpenEditor is a seam so tests can stub it out. The default opens $EDITOR
// (falling back to "vi") with stdin/stdout/stderr attached so the user gets
// a real interactive edit session.
var OpenEditor = func(path string) error {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// secretRe matches environment variable names that look like secrets.
var secretRe = regexp.MustCompile(`(?i)password|secret|token|_key`)

// imageInspectResult is the minimal shape we need from `image inspect` JSON.
type imageInspectResult struct {
	Config struct {
		ExposedPorts map[string]struct{} `json:"ExposedPorts"`
		Env          []string            `json:"Env"`
		Volumes      map[string]struct{} `json:"Volumes"`
	} `json:"Config"`
}

// DeriveFromImageConfig is the pure core: given `<rt> image inspect <ref>`
// JSON (array; [0].Config used), produce compose.yml + values.yaml.
//
// Rules:
//   - service name = template name; image: the literal ref.
//   - Config.ExposedPorts ("5432/tcp") → expose: entries (sorted numeric).
//   - Config.Volumes → named volumes data0..dataN mapped to each path (sorted).
//   - Config.Env "K=V" → values key lower(k) with default V, EXCEPT skip PATH;
//     secret-looking keys (case-insensitive regex `password|secret|token|_key`)
//     get value "change-me" in values.yaml; compose env renders K: ${K:-<default>}.
//   - Optional knobs present but COMMENTED at the end of the service block.
//   - values.yaml gets description: "scaffolded from <imageRef>".
func DeriveFromImageConfig(name, imageRef string, inspectJSON []byte) (composeYML, valuesYML []byte, err error) {
	var results []imageInspectResult
	if err := json.Unmarshal(inspectJSON, &results); err != nil {
		return nil, nil, fmt.Errorf("parsing image inspect JSON: %w", err)
	}
	if len(results) == 0 {
		return nil, nil, fmt.Errorf("image inspect JSON is empty")
	}
	cfg := results[0].Config

	// --- Exposed ports (sorted numerically) ---
	var ports []int
	for p := range cfg.ExposedPorts {
		// p is like "5432/tcp"
		parts := strings.SplitN(p, "/", 2)
		n, err := strconv.Atoi(parts[0])
		if err == nil {
			ports = append(ports, n)
		}
	}
	sort.Ints(ports)

	// --- Volumes (sorted by path, named data0..dataN) ---
	var volPaths []string
	for vp := range cfg.Volumes {
		volPaths = append(volPaths, vp)
	}
	sort.Strings(volPaths)

	// --- Env vars ---
	type envEntry struct {
		key      string // original uppercase key
		lkey     string // lowercase key (values.yaml key)
		def      string // default value from image
		isSecret bool
	}
	var envEntries []envEntry
	for _, e := range cfg.Env {
		idx := strings.IndexByte(e, '=')
		var k, v string
		if idx < 0 {
			k = e
			v = ""
		} else {
			k = e[:idx]
			v = e[idx+1:]
		}
		// Skip PATH
		if strings.ToUpper(k) == "PATH" {
			continue
		}
		lk := strings.ToLower(k)
		isSecret := secretRe.MatchString(k)
		envEntries = append(envEntries, envEntry{
			key:      k,
			lkey:     lk,
			def:      v,
			isSecret: isSecret,
		})
	}

	// --- Build compose.yml ---
	var cb bytes.Buffer
	cb.WriteString("services:\n")
	cb.WriteString(fmt.Sprintf("  %s:\n", name))
	cb.WriteString(fmt.Sprintf("    image: %s\n", imageRef))

	// environment section
	if len(envEntries) > 0 {
		cb.WriteString("    environment:\n")
		for _, e := range envEntries {
			cb.WriteString(fmt.Sprintf("      %s: ${%s:-%s}\n", e.key, e.key, e.def))
		}
	}

	// volumes mounts
	if len(volPaths) > 0 {
		cb.WriteString("    volumes:\n")
		for i, vp := range volPaths {
			cb.WriteString(fmt.Sprintf("      - data%d:%s\n", i, vp))
		}
	}

	// expose
	if len(ports) > 0 {
		cb.WriteString("    expose:\n")
		for _, p := range ports {
			cb.WriteString(fmt.Sprintf("      - \"%d\"\n", p))
		}
	}

	// commented knobs block
	cb.WriteString("    # deploy:\n")
	cb.WriteString("    #   resources:\n")
	cb.WriteString("    #     limits:\n")
	cb.WriteString("    #       cpus: \"2\"\n")
	cb.WriteString("    #       memory: 1g\n")
	cb.WriteString("    # restart: unless-stopped\n")
	cb.WriteString("    # healthcheck:\n")
	cb.WriteString("    #   test: [\"CMD\", \"...\"]\n")
	cb.WriteString("    #   interval: 10s\n")
	cb.WriteString("    # # extra mount example:\n")
	cb.WriteString("    # volumes:\n")
	cb.WriteString("    #   - ./config:/etc/myapp:ro\n")

	// named volumes section
	if len(volPaths) > 0 {
		cb.WriteString("volumes:\n")
		for i := range volPaths {
			cb.WriteString(fmt.Sprintf("  data%d: {}\n", i))
		}
	}

	// --- Build values.yaml ---
	var vb bytes.Buffer
	vb.WriteString(fmt.Sprintf("description: \"scaffolded from %s\"\n", imageRef))
	for _, e := range envEntries {
		val := e.def
		if e.isSecret {
			val = "change-me"
		}
		// Quote value if it looks numeric to preserve it as a string,
		// or if it contains special chars. Use YAML block scalar.
		vb.WriteString(fmt.Sprintf("%s: %s\n", e.lkey, yamlScalar(val)))
	}

	return cb.Bytes(), vb.Bytes(), nil
}

// yamlScalar produces a minimal safe YAML scalar for val.
// Numeric strings and booleans need quoting to stay as strings.
func yamlScalar(val string) string {
	if val == "" {
		return `""`
	}
	// If it parses as a number or bool keyword, quote it.
	if _, err := strconv.ParseFloat(val, 64); err == nil {
		return fmt.Sprintf("%q", val)
	}
	switch strings.ToLower(val) {
	case "true", "false", "yes", "no", "null", "~":
		return fmt.Sprintf("%q", val)
	}
	return val
}

// ValidateCompose runs `<rt> compose -p kazi-template-check
// --project-directory <dir> -f <dir>/compose.yml config --quiet`
// via compose.Output. Returns nil on success, ErrInvalidTemplate on failure.
func ValidateCompose(ctx context.Context, rt runtime.Runtime, dir string) error {
	composeFile := filepath.Join(dir, "compose.yml")
	cmd := rt.ComposeCmd(ctx, "kazi-template-check", dir, []string{composeFile}, "config", "--quiet")
	_, err := compose.Output(cmd)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidTemplate, err)
	}
	return nil
}

// Scaffold pulls the image (streamed), inspects it, derives a template,
// writes it to Dir()/<name>/, opens $EDITOR on compose.yml, then validates.
//
//   - Existing dir → error.
//   - Pull is streamed via compose.Run (using pull subcommand on a minimal compose file).
//   - Inspect via rt.Cmd("image","inspect",ref).
//   - Editor abort (empty file after editor) → remove dir, return ErrAborted.
//   - Validation failure → return ErrInvalidTemplate (caller offers re-edit/abort).
//
// Returns the template directory path on success.
func Scaffold(ctx context.Context, rt runtime.Runtime, name, imageRef string, out, errW io.Writer) (string, error) {
	templateDir := filepath.Join(Dir(), name)

	// Existing dir → error
	if _, err := os.Stat(templateDir); err == nil {
		return "", fmt.Errorf("template %q already exists at %s", name, templateDir)
	}

	// Pull the image, streaming output
	if err := pullImage(ctx, rt, imageRef, out, errW); err != nil {
		return "", fmt.Errorf("pulling image %s: %w", imageRef, err)
	}

	// Inspect the image
	inspectCmd := rt.Cmd(ctx, "image", "inspect", imageRef)
	inspectJSON, err := compose.Output(inspectCmd)
	if err != nil {
		return "", fmt.Errorf("inspecting image %s: %w", imageRef, err)
	}

	// Derive compose.yml + values.yaml from the image config
	composeYML, valuesYML, err := DeriveFromImageConfig(name, imageRef, []byte(inspectJSON))
	if err != nil {
		return "", fmt.Errorf("deriving template from image config: %w", err)
	}

	// Write template files
	if err := os.MkdirAll(templateDir, 0o755); err != nil {
		return "", fmt.Errorf("creating template dir: %w", err)
	}

	composePath := filepath.Join(templateDir, "compose.yml")
	if err := os.WriteFile(composePath, composeYML, 0o644); err != nil {
		os.RemoveAll(templateDir)
		return "", fmt.Errorf("writing compose.yml: %w", err)
	}

	valuesPath := filepath.Join(templateDir, "values.yaml")
	if err := os.WriteFile(valuesPath, valuesYML, 0o644); err != nil {
		os.RemoveAll(templateDir)
		return "", fmt.Errorf("writing values.yaml: %w", err)
	}

	// Open editor
	if err := OpenEditor(composePath); err != nil {
		os.RemoveAll(templateDir)
		return "", fmt.Errorf("editor exited with error: %w", err)
	}

	// Abort if the file is empty after editing
	fi, err := os.Stat(composePath)
	if err != nil || fi.Size() == 0 {
		os.RemoveAll(templateDir)
		return "", ErrAborted
	}

	// Validate
	if err := ValidateCompose(ctx, rt, templateDir); err != nil {
		return "", err
	}

	return templateDir, nil
}

// pullImage pulls an image by constructing a minimal compose file and running
// `compose pull`, streaming output to out/errW.
func pullImage(ctx context.Context, rt runtime.Runtime, imageRef string, out, errW io.Writer) error {
	// Use a minimal single-service compose content to drive `compose pull`.
	// We write it to a temp file so compose can parse it.
	tmp, err := os.CreateTemp("", "kazi-pull-*.yml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	content := fmt.Sprintf("services:\n  pull:\n    image: %s\n", imageRef)
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()

	tmpDir := filepath.Dir(tmp.Name())
	cmd := rt.ComposeCmd(ctx, "kazi-pull", tmpDir, []string{tmp.Name()}, "pull")
	return compose.Run(cmd, out, errW)
}
