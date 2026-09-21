// Package apicontract validates the versioned OpenAPI contract and keeps it
// synchronized with the routes registered by backend modules.
package apicontract

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Document struct {
	OpenAPI string `json:"openapi"`
	Info    struct {
		Title   string `json:"title"`
		Version string `json:"version"`
	} `json:"info"`
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas       map[string]json.RawMessage `json:"schemas"`
		RequestBodies map[string]json.RawMessage `json:"requestBodies"`
		Responses     map[string]json.RawMessage `json:"responses"`
	} `json:"components"`
}

type Operation struct {
	Method string
	Path   string
}

func (o Operation) String() string { return o.Method + " " + o.Path }

func Load(path string) (Document, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Document{}, fmt.Errorf("read API contract: %w", err)
	}
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return Document{}, fmt.Errorf("decode API contract: %w", err)
	}
	return document, nil
}

func Validate(document Document) error {
	if document.OpenAPI != "3.1.0" {
		return fmt.Errorf("openapi must be 3.1.0, got %q", document.OpenAPI)
	}
	if document.Info.Title == "" || !strings.HasPrefix(document.Info.Version, "1.") {
		return fmt.Errorf("contract must have a title and a version in the 1.x line")
	}
	for _, name := range []string{"Error", "FieldError", "Pagination", "Money", "Date", "DateTime", "UUID", "UploadMetadata"} {
		if _, ok := document.Components.Schemas[name]; !ok {
			return fmt.Errorf("required schema %q is missing", name)
		}
	}
	if _, ok := document.Components.RequestBodies["FileUpload"]; !ok {
		return fmt.Errorf("required FileUpload request body is missing")
	}
	if _, ok := document.Components.Responses["BinaryDownload"]; !ok {
		return fmt.Errorf("required BinaryDownload response is missing")
	}
	if len(document.Paths) == 0 {
		return fmt.Errorf("contract has no paths")
	}
	for path, pathItem := range document.Paths {
		if !strings.HasPrefix(path, "/api/") {
			return fmt.Errorf("path %q must start with /api/", path)
		}
		for method, operation := range pathItem {
			if method == "parameters" || method == "$ref" {
				continue
			}
			upper := strings.ToUpper(method)
			if !isHTTPMethod(upper) {
				return fmt.Errorf("unsupported operation key %q for %s", method, path)
			}
			var body struct {
				Responses map[string]json.RawMessage `json:"responses"`
			}
			if err := json.Unmarshal(operation, &body); err != nil || len(body.Responses) == 0 {
				return fmt.Errorf("%s %s must declare responses", upper, path)
			}
		}
	}
	return nil
}

func Operations(document Document) []Operation {
	operations := make([]Operation, 0)
	for path, pathItem := range document.Paths {
		for method := range pathItem {
			upper := strings.ToUpper(method)
			if isHTTPMethod(upper) {
				operations = append(operations, Operation{Method: upper, Path: path})
			}
		}
	}
	sortOperations(operations)
	return operations
}

var routePattern = regexp.MustCompile(`HandleFunc\("([A-Z]+) (/api/[^" ]+)"`)

func DiscoverModuleRoutes(root string) ([]Operation, error) {
	seen := make(map[Operation]string)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range routePattern.FindAllStringSubmatch(string(data), -1) {
			operation := Operation{Method: match[1], Path: match[2]}
			if previous, exists := seen[operation]; exists {
				return fmt.Errorf("duplicate route %s in %s and %s", operation, previous, path)
			}
			seen[operation] = path
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("discover module routes: %w", err)
	}
	operations := make([]Operation, 0, len(seen))
	for operation := range seen {
		operations = append(operations, operation)
	}
	sortOperations(operations)
	return operations, nil
}

func CompareRoutes(contract, registered []Operation) error {
	want := make(map[Operation]struct{}, len(contract))
	got := make(map[Operation]struct{}, len(registered))
	for _, operation := range contract {
		want[operation] = struct{}{}
	}
	for _, operation := range registered {
		got[operation] = struct{}{}
	}
	var missing, stale []string
	for operation := range got {
		if _, ok := want[operation]; !ok {
			missing = append(missing, operation.String())
		}
	}
	for operation := range want {
		if _, ok := got[operation]; !ok {
			stale = append(stale, operation.String())
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)
	if len(missing) > 0 || len(stale) > 0 {
		return fmt.Errorf("route contract mismatch; undocumented=%v, stale=%v", missing, stale)
	}
	return nil
}

func isHTTPMethod(method string) bool {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	default:
		return false
	}
}

func sortOperations(operations []Operation) {
	sort.Slice(operations, func(i, j int) bool { return operations[i].String() < operations[j].String() })
}
