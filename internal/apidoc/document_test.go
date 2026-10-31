package apidoc_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/labstack/echo/v5"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"go.uber.org/mock/gomock"
	"smapass-back/internal/apidoc"
	"smapass-back/internal/domain/repository/mock"
	"smapass-back/internal/infrastructure/router"
	"smapass-back/internal/interface/controller"
	"smapass-back/internal/usecase"
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
		{"certificate.json", "Certificate"}, {"records.json", "Records"},
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
			if test.schema == "Certificate" {
				example.(map[string]any)["listing"].(map[string]any)["imei_suffix"] = "123456789012345"
				if err = definition.Validate(example); err == nil {
					t.Fatal("public schema accepted a full IMEI")
				}
			}
		})
	}
}

func TestDocumentedRoutesMatchRouter(t *testing.T) {
	paths := generatedDocument(t)["paths"].(map[string]any)
	repo := mock.NewMockCertificateRepository(gomock.NewController(t))
	app := router.New(controller.NewCertificateController(usecase.NewCertificateUsecase(repo)), controller.NewHealthController(nil), nil)
	actual := map[string]bool{}
	for _, route := range app.Router().Routes() {
		// Echo also registers internal group fallback routes; only concrete routes are API operations.
		if route.Method == echo.RouteNotFound || strings.Contains(route.Path, "*") || !strings.HasPrefix(route.Path, "/") {
			continue
		}
		path := strings.ReplaceAll(route.Path, ":number", "{number}")
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
