package apicontract

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type goStructRef struct {
	File string `json:"file"`
	Name string `json:"name"`
}

type dtoSchema struct {
	GoStruct             *goStructRef               `json:"x-go-struct"`
	Properties           map[string]json.RawMessage `json:"properties"`
	AdditionalProperties json.RawMessage            `json:"additionalProperties"`
}

type dtoDocument struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas       map[string]dtoSchema       `json:"schemas"`
		RequestBodies map[string]json.RawMessage `json:"requestBodies"`
		Responses     map[string]json.RawMessage `json:"responses"`
	} `json:"components"`
}

type discoveredDTO struct {
	File   string
	Name   string
	Fields []string
}

// QA-08: OpenAPI не должен расходиться с фактическими DTO backend.
// Тест парсит реальные Go-структуры request/response в handlers и сверяет их
// с component schemas, помеченными x-go-struct.
func TestOpenAPISchemasMatchHandlerDTOs(t *testing.T) {
	repoRoot := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(repoRoot, "api", "openapi", "v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var document dtoDocument
	if err := json.Unmarshal(raw, &document); err != nil {
		t.Fatal(err)
	}

	discovered, err := discoverHandlerDTOs(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	covered := make(map[string]string)
	requestSchemas := make(map[string]struct{})
	responseSchemas := make(map[string]struct{})
	for schemaName, schema := range document.Components.Schemas {
		if schema.GoStruct == nil {
			continue
		}
		key := schema.GoStruct.File + ":" + schema.GoStruct.Name
		dto, ok := discovered[key]
		if !ok {
			t.Fatalf("schema %s references missing Go DTO %s", schemaName, key)
		}
		if err := compareFieldSets(dto.Fields, propertyNames(schema.Properties)); err != nil {
			t.Fatalf("schema %s does not match %s: %v", schemaName, key, err)
		}
		if !isBooleanFalse(schema.AdditionalProperties) {
			t.Fatalf("schema %s must set additionalProperties=false for strict DTO drift detection", schemaName)
		}
		covered[key] = schemaName
		if strings.HasSuffix(schema.GoStruct.Name, "Request") {
			requestSchemas[schemaName] = struct{}{}
		}
		if strings.HasSuffix(schema.GoStruct.Name, "Response") {
			responseSchemas[schemaName] = struct{}{}
		}
	}
	for key := range discovered {
		if _, ok := covered[key]; !ok {
			t.Fatalf("Go DTO %s has no OpenAPI schema with x-go-struct", key)
		}
	}

	usedRequests, usedResponses := operationSchemaRefs(document)
	for schemaName := range requestSchemas {
		if _, ok := usedRequests[schemaName]; !ok {
			t.Fatalf("request schema %s is not used by any operation requestBody", schemaName)
		}
	}
	for schemaName := range responseSchemas {
		if _, ok := usedResponses[schemaName]; !ok {
			t.Fatalf("response schema %s is not used by any operation response", schemaName)
		}
	}
}

func discoverHandlerDTOs(repoRoot string) (map[string]discoveredDTO, error) {
	root := filepath.Join(repoRoot, "backend", "internal", "handlers")
	out := make(map[string]discoveredDTO)
	fileset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fileset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(repoRoot, path)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				typeSpec := spec.(*ast.TypeSpec)
				if !strings.HasSuffix(typeSpec.Name.Name, "Request") && !strings.HasSuffix(typeSpec.Name.Name, "Response") {
					continue
				}
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					continue
				}
				fields := jsonFields(structType)
				if len(fields) == 0 {
					continue
				}
				sort.Strings(fields)
				key := relative + ":" + typeSpec.Name.Name
				out[key] = discoveredDTO{File: relative, Name: typeSpec.Name.Name, Fields: fields}
			}
		}
		return nil
	})
	return out, err
}

func jsonFields(structType *ast.StructType) []string {
	fields := make([]string, 0, len(structType.Fields.List))
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 || field.Tag == nil {
			continue
		}
		tag := strings.Trim(field.Tag.Value, "`")
		name := strings.Split(reflect.StructTag(tag).Get("json"), ",")[0]
		if name == "" || name == "-" {
			continue
		}
		fields = append(fields, name)
	}
	return fields
}

func propertyNames(properties map[string]json.RawMessage) []string {
	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func compareFieldSets(goFields, schemaFields []string) error {
	if strings.Join(goFields, "\x00") == strings.Join(schemaFields, "\x00") {
		return nil
	}
	return fmt.Errorf("Go fields=%v, schema properties=%v", goFields, schemaFields)
}

func isBooleanFalse(raw json.RawMessage) bool {
	var value bool
	return len(raw) > 0 && json.Unmarshal(raw, &value) == nil && !value
}

func operationSchemaRefs(document dtoDocument) (map[string]struct{}, map[string]struct{}) {
	requests := map[string]struct{}{}
	responses := map[string]struct{}{}
	for _, pathItem := range document.Paths {
		for method, rawOperation := range pathItem {
			if !isHTTPMethod(strings.ToUpper(method)) {
				continue
			}
			var operation struct {
				RequestBody json.RawMessage            `json:"requestBody"`
				Responses   map[string]json.RawMessage `json:"responses"`
			}
			if json.Unmarshal(rawOperation, &operation) != nil {
				continue
			}
			if ref := schemaRefFromRequestBody(operation.RequestBody, document.Components.RequestBodies); ref != "" {
				requests[ref] = struct{}{}
			}
			for _, rawResponse := range operation.Responses {
				if ref := schemaRefFromResponse(rawResponse, document.Components.Responses); ref != "" {
					responses[ref] = struct{}{}
				}
			}
		}
	}
	return requests, responses
}

func schemaRefFromRequestBody(raw json.RawMessage, components map[string]json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var body struct {
		Ref     string `json:"$ref"`
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	if body.Ref != "" {
		name := strings.TrimPrefix(body.Ref, "#/components/requestBodies/")
		if component, ok := components[name]; ok {
			return schemaRefFromRequestBody(component, components)
		}
		return ""
	}
	if media, ok := body.Content["application/json"]; ok {
		return schemaName(media.Schema)
	}
	return ""
}

func schemaRefFromResponse(raw json.RawMessage, components map[string]json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var response struct {
		Ref     string `json:"$ref"`
		Content map[string]struct {
			Schema json.RawMessage `json:"schema"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &response) != nil {
		return ""
	}
	if response.Ref != "" {
		name := strings.TrimPrefix(response.Ref, "#/components/responses/")
		if component, ok := components[name]; ok {
			return schemaRefFromResponse(component, components)
		}
		return ""
	}
	if media, ok := response.Content["application/json"]; ok {
		return schemaName(media.Schema)
	}
	return ""
}

func schemaName(raw json.RawMessage) string {
	var schema struct {
		Ref string `json:"$ref"`
	}
	if json.Unmarshal(raw, &schema) != nil || schema.Ref == "" {
		return ""
	}
	return strings.TrimPrefix(schema.Ref, "#/components/schemas/")
}
