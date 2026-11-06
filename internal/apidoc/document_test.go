package apidoc_test

import (
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"smapass-back/internal/apidoc"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
)

func generatedDocument(t *testing.T) map[string]any {
	t.Helper()
	source, err := os.ReadFile("../../docs/openapi.base.json")
	if err != nil {
		t.Fatal(err)
	}
	data, err := apidoc.Generate(source)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	return document
}

func TestPublicExamplesMatchOpenAPI(t *testing.T) {
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource("https://smapass.test/openapi.json", generatedDocument(t)); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ file, schema string }{
		{"records.json", "Records"}, {"device-state.json", "DeviceState"}, {"measurement-request.json", "SubmitMeasurement"},
	} {
		t.Run(test.file, func(t *testing.T) {
			definition, err := compiler.Compile("https://smapass.test/openapi.json#/components/schemas/" + test.schema)
			if err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile("../../docs/examples/" + test.file)
			if err != nil {
				t.Fatal(err)
			}
			var example any
			if err = json.Unmarshal(data, &example); err != nil {
				t.Fatal(err)
			}
			if err = definition.Validate(example); err != nil {
				t.Fatal(err)
			}

		})
	}
}

func TestDocumentedRoutesMatchRouter(t *testing.T) {
	paths := generatedDocument(t)["paths"].(map[string]any)
	app := router.New(controller.NewHealthController(nil), nil)
	router.AddDeviceRoutes(app, controller.NewDeviceController(nil), func(next echo.HandlerFunc) echo.HandlerFunc { return next })
	router.AddAuthRoutes(app, controller.NewAuthController(nil))
	actual := map[string]bool{}
	for _, route := range app.Router().Routes() {
		// Echo also registers internal group fallback routes; only concrete routes are API operations.
		if route.Method == echo.RouteNotFound || strings.Contains(route.Path, "*") || !strings.HasPrefix(route.Path, "/") {
			continue
		}
		path := regexp.MustCompile(`:([^/]+)`).ReplaceAllString(route.Path, `{$1}`)
		method := strings.ToLower(route.Method)
		operations, exists := paths[path].(map[string]any)
		if !exists {
			t.Fatalf("undocumented route %s %s", method, path)
		}
		if _, exists := operations[method]; !exists {
			t.Fatalf("undocumented method %s %s", method, path)
		}
		actual[method+" "+path] = true
	}
	for path, operations := range paths {
		for method := range operations.(map[string]any) {
			if !actual[method+" "+path] {
				t.Fatalf("documented operation is not registered: %s %s", method, path)
			}
		}
	}
}
