package oas

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/mozership/probe/yamlfmt"
	"github.com/pb33f/libopenapi"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"
)

const defaultServerURL = "http://localhost:8080"

// Generate reads an OpenAPI spec file and generates a probe Workflow YAML string.
func Generate(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read OpenAPI spec: %w", err)
	}

	return GenerateFromBytes(data)
}

// GenerateFromBytes generates a probe Workflow YAML from OpenAPI spec bytes.
func GenerateFromBytes(data []byte) (string, error) {
	doc, err := libopenapi.NewDocument(data)
	if err != nil {
		return "", fmt.Errorf("failed to parse OpenAPI spec: %w", err)
	}

	model, err := doc.BuildV3Model()
	if err != nil {
		return "", fmt.Errorf("failed to build OpenAPI model: %w", err)
	}

	wf := buildWorkflow(model)

	out, err := yaml.Marshal(wf)
	if err != nil {
		return "", fmt.Errorf("failed to marshal workflow: %w", err)
	}

	// The workflow is laid out as probe fmt lays one out.
	out, err = yamlfmt.Format(out)
	if err != nil {
		return "", fmt.Errorf("failed to format workflow: %w", err)
	}

	return string(out), nil
}

// workflowMap represents the workflow structure for YAML output.
// We use map-based types to control field ordering in YAML output.
type workflowMap struct {
	Name string         `yaml:"name"`
	Vars map[string]any `yaml:"vars,omitempty"`
	Jobs []jobMap       `yaml:"jobs"`
}

type jobMap struct {
	Name     string    `yaml:"name"`
	ID       string    `yaml:"id"`
	Defaults any       `yaml:"defaults,omitempty"`
	Steps    []stepMap `yaml:"steps"`
}

type stepMap struct {
	Name string         `yaml:"name"`
	ID   string         `yaml:"id"`
	Uses string         `yaml:"uses"`
	With map[string]any `yaml:"with"`
	Vars map[string]any `yaml:"vars,omitempty"`
	Test string         `yaml:"test"`
}

func buildWorkflow(model *libopenapi.DocumentModel[v3.Document]) workflowMap {
	doc := model.Model

	title := "API"
	if doc.Info != nil && doc.Info.Title != "" {
		title = doc.Info.Title
	}

	serverURL := defaultServerURL
	if len(doc.Servers) > 0 && doc.Servers[0].URL != "" {
		serverURL = doc.Servers[0].URL
	}

	vars := map[string]any{
		"base_url": fmt.Sprintf("{{BASE_URL ?? '%s'}}", serverURL),
	}

	// Detect authentication schemes
	authType := detectAuth(doc)
	if authType == authBearer || authType == authOAuth2 {
		vars["auth_token"] = "{{AUTH_TOKEN ?? ''}}"
	}
	if authType == authAPIKeyHeader || authType == authAPIKeyQuery {
		vars["api_key"] = "{{API_KEY ?? ''}}"
	}

	// Build operations from paths
	ops := extractOperations(doc)

	// Group operations into jobs
	jobs := groupOperationsIntoJobs(ops, authType, doc)

	return workflowMap{
		Name: title + " API Tests",
		Vars: vars,
		Jobs: jobs,
	}
}

type authKind int

const (
	authNone authKind = iota
	authBearer
	authAPIKeyHeader
	authAPIKeyQuery
	authOAuth2
)

type apiKeyInfo struct {
	name string
}

func detectAuth(doc v3.Document) authKind {
	if doc.Components == nil || doc.Components.SecuritySchemes == nil {
		return authNone
	}

	for pair := doc.Components.SecuritySchemes.First(); pair != nil; pair = pair.Next() {
		scheme := pair.Value()
		switch {
		case scheme.Type == "http" && strings.ToLower(scheme.Scheme) == "bearer":
			return authBearer
		case scheme.Type == "oauth2":
			return authOAuth2
		case scheme.Type == "apiKey" && strings.ToLower(scheme.In) == "header":
			return authAPIKeyHeader
		case scheme.Type == "apiKey" && strings.ToLower(scheme.In) == "query":
			return authAPIKeyQuery
		}
	}

	return authNone
}

func getAPIKeyInfo(doc v3.Document) *apiKeyInfo {
	if doc.Components == nil || doc.Components.SecuritySchemes == nil {
		return nil
	}
	for pair := doc.Components.SecuritySchemes.First(); pair != nil; pair = pair.Next() {
		scheme := pair.Value()
		if scheme.Type == "apiKey" {
			return &apiKeyInfo{name: scheme.Name}
		}
	}
	return nil
}

type operation struct {
	path      string
	method    string
	summary   string
	opID      string
	tags      []string
	params    []*v3.Parameter
	body      *v3.RequestBody
	responses *v3.Responses
}

func extractOperations(doc v3.Document) []operation {
	var ops []operation

	if doc.Paths == nil || doc.Paths.PathItems == nil {
		return ops
	}

	for pair := doc.Paths.PathItems.First(); pair != nil; pair = pair.Next() {
		path := pair.Key()
		item := pair.Value()

		methods := []struct {
			name string
			op   *v3.Operation
		}{
			{"get", item.Get},
			{"post", item.Post},
			{"put", item.Put},
			{"patch", item.Patch},
			{"delete", item.Delete},
			{"head", item.Head},
			{"options", item.Options},
		}

		for _, m := range methods {
			if m.op == nil {
				continue
			}
			op := operation{
				path:      path,
				method:    m.name,
				summary:   m.op.Summary,
				opID:      m.op.OperationId,
				tags:      m.op.Tags,
				responses: m.op.Responses,
			}
			if m.op.Parameters != nil {
				op.params = m.op.Parameters
			}
			// Merge path-level parameters
			if item.Parameters != nil {
				op.params = mergeParameters(op.params, item.Parameters)
			}
			if m.op.RequestBody != nil {
				op.body = m.op.RequestBody
			}
			ops = append(ops, op)
		}
	}

	return ops
}

// mergeParameters merges path-level parameters with operation-level parameters.
// Operation-level parameters take precedence.
func mergeParameters(opParams, pathParams []*v3.Parameter) []*v3.Parameter {
	existing := make(map[string]bool)
	for _, p := range opParams {
		existing[p.Name+":"+p.In] = true
	}
	for _, p := range pathParams {
		key := p.Name + ":" + p.In
		if !existing[key] {
			opParams = append(opParams, p)
		}
	}
	return opParams
}

func groupOperationsIntoJobs(ops []operation, auth authKind, doc v3.Document) []jobMap {
	// Try grouping by tags first
	tagGroups := make(map[string][]operation)
	noTagOps := []operation{}

	for _, op := range ops {
		if len(op.tags) > 0 {
			tag := op.tags[0]
			tagGroups[tag] = append(tagGroups[tag], op)
		} else {
			noTagOps = append(noTagOps, op)
		}
	}

	if len(tagGroups) > 0 {
		// Add untagged operations to a default group
		if len(noTagOps) > 0 {
			tagGroups["default"] = append(tagGroups["default"], noTagOps...)
		}
		return buildJobsFromGroups(tagGroups, auth, doc)
	}

	// Fallback: group by path prefix
	prefixGroups := make(map[string][]operation)
	for _, op := range ops {
		prefix := pathPrefix(op.path)
		prefixGroups[prefix] = append(prefixGroups[prefix], op)
	}

	if len(prefixGroups) > 1 {
		return buildJobsFromGroups(prefixGroups, auth, doc)
	}

	// Final fallback: single job
	single := map[string][]operation{"api": ops}
	return buildJobsFromGroups(single, auth, doc)
}

func pathPrefix(path string) string {
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	if len(parts) > 0 && parts[0] != "" {
		return parts[0]
	}
	return "api"
}

func buildJobsFromGroups(groups map[string][]operation, auth authKind, doc v3.Document) []jobMap {
	// Sort group keys for deterministic output
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	usedIDs := make(map[string]int)
	var jobs []jobMap

	for _, groupName := range keys {
		ops := groups[groupName]

		defaults := buildDefaults(auth, doc)

		steps := buildSteps(ops, usedIDs)

		jobs = append(jobs, jobMap{
			Name:     toTitle(groupName),
			ID:       toID(groupName),
			Defaults: defaults,
			Steps:    steps,
		})
	}

	return jobs
}

func buildDefaults(auth authKind, doc v3.Document) map[string]any {
	headers := map[string]any{
		"content-type": "application/json",
		"accept":       "application/json",
	}

	switch auth {
	case authBearer, authOAuth2:
		headers["authorization"] = "Bearer {{vars.auth_token}}"
	case authAPIKeyHeader:
		info := getAPIKeyInfo(doc)
		if info != nil {
			headers[strings.ToLower(info.name)] = "{{vars.api_key}}"
		}
	}

	httpDefaults := map[string]any{
		"url":     "{{vars.base_url}}",
		"headers": headers,
	}

	if auth == authAPIKeyQuery {
		info := getAPIKeyInfo(doc)
		if info != nil {
			httpDefaults["query"] = map[string]any{
				info.name: "{{vars.api_key}}",
			}
		}
	}

	return map[string]any{
		"http": httpDefaults,
	}
}

func buildSteps(ops []operation, usedIDs map[string]int) []stepMap {
	var steps []stepMap

	for _, op := range ops {
		step := buildStep(op, usedIDs)
		steps = append(steps, step)
	}

	return steps
}

func buildStep(op operation, usedIDs map[string]int) stepMap {
	name := op.summary
	if name == "" {
		name = fmt.Sprintf("%s %s", strings.ToUpper(op.method), op.path)
	}

	id := generateStepID(op, usedIDs)

	with := make(map[string]any)
	vars := make(map[string]any)

	// Build path with variable substitution
	pathStr := op.path
	pathParams, queryParams := classifyParams(op.params)

	for _, p := range pathParams {
		varName := toSnakeCase(p.Name)
		placeholder := fmt.Sprintf("{{vars.%s}}", varName)
		pathStr = strings.ReplaceAll(pathStr, "{"+p.Name+"}", placeholder)
		vars[varName] = pathParamExample(p)
	}

	// Append query parameters to path
	if len(queryParams) > 0 {
		qparts := make([]string, 0, len(queryParams))
		for _, p := range queryParams {
			qparts = append(qparts, fmt.Sprintf("%s=%v", p.Name, paramExample(p)))
		}
		pathStr = pathStr + "?" + strings.Join(qparts, "&")
	}

	with[op.method] = pathStr

	// Request body
	if op.body != nil && op.body.Content != nil {
		if ct, ok := op.body.Content.Get("application/json"); ok {
			if ct.Schema != nil {
				schema := ct.Schema.Schema()
				if schema != nil {
					body := generateExample(schema, 0)
					if body != nil {
						with["body"] = body
					}
				}
			}
		} else if _, ok := op.body.Content.Get("multipart/form-data"); ok {
			// Skip multipart body, add note to name
			name = name + " (multipart)"
		}
	}

	// Test expression
	test := buildTestExpression(op.responses)

	step := stepMap{
		Name: name,
		ID:   id,
		Uses: "http",
		With: with,
		Test: test,
	}

	if len(vars) > 0 {
		step.Vars = vars
	}

	return step
}

func generateStepID(op operation, usedIDs map[string]int) string {
	var id string
	if op.opID != "" {
		id = toID(op.opID)
	} else {
		id = toID(op.method + "-" + strings.ReplaceAll(strings.TrimPrefix(op.path, "/"), "/", "-"))
	}

	// Handle duplicates
	if count, exists := usedIDs[id]; exists {
		usedIDs[id] = count + 1
		id = fmt.Sprintf("%s-%d", id, count+1)
	} else {
		usedIDs[id] = 1
	}

	return id
}

func classifyParams(params []*v3.Parameter) (pathParams, queryParams []*v3.Parameter) {
	for _, p := range params {
		switch p.In {
		case "path":
			pathParams = append(pathParams, p)
		case "query":
			queryParams = append(queryParams, p)
		}
	}
	return
}

// pathParamExample returns a string example value for path parameters.
func pathParamExample(p *v3.Parameter) string {
	if p.Example != nil {
		return fmt.Sprintf("%v", nodeToValue(p.Example))
	}
	if p.Schema != nil {
		schema := p.Schema.Schema()
		if schema != nil {
			t := schemaTypeName(schema)
			if t == "integer" || t == "number" {
				return "1"
			}
		}
	}
	return "example"
}

func paramExample(p *v3.Parameter) any {
	if p.Example != nil {
		return nodeToValue(p.Example)
	}
	if p.Schema != nil {
		schema := p.Schema.Schema()
		if schema != nil {
			return generateExample(schema, 0)
		}
	}
	return "example"
}

func buildTestExpression(responses *v3.Responses) string {
	if responses == nil || responses.Codes == nil {
		return "res.code >= 200 && res.code < 300"
	}

	// Find the first 2xx response code
	for pair := responses.Codes.First(); pair != nil; pair = pair.Next() {
		code := pair.Key()
		if len(code) == 3 && code[0] == '2' {
			if _, err := strconv.Atoi(code); err == nil {
				return fmt.Sprintf("res.code == %s", code)
			}
		}
	}

	return "res.code >= 200 && res.code < 300"
}

// toTitle converts a string to title case (e.g., "pet-store" → "Pet Store")
func toTitle(s string) string {
	s = strings.ReplaceAll(s, "-", " ")
	s = strings.ReplaceAll(s, "_", " ")
	words := strings.Fields(s)
	for i, w := range words {
		if len(w) > 0 {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}
	return strings.Join(words, " ")
}

// toID converts a string to a kebab-case ID (e.g., "listPets" → "list-pets")
func toID(s string) string {
	// Convert camelCase/PascalCase to kebab-case
	s = camelToKebab(s)
	s = strings.ToLower(s)
	// Replace non-alphanumeric characters with hyphens
	re := regexp.MustCompile(`[^a-z0-9]+`)
	s = re.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}

// camelToKebab converts camelCase to kebab-case
func camelToKebab(s string) string {
	re := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	return re.ReplaceAllString(s, "${1}-${2}")
}

// toSnakeCase converts a string to snake_case
func toSnakeCase(s string) string {
	re := regexp.MustCompile(`([a-z0-9])([A-Z])`)
	s = re.ReplaceAllString(s, "${1}_${2}")
	s = strings.ToLower(s)
	re2 := regexp.MustCompile(`[^a-z0-9]+`)
	s = re2.ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	return s
}
