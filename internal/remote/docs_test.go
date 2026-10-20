package remote

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"yovoice/internal/schema"
	"yovoice/internal/workbench"
)

type openAPIDoc struct {
	Paths      map[string]map[string]json.RawMessage `json:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

func TestDocsServedWithoutToken(t *testing.T) {
	api := &API{Token: strings.Repeat("a", 32)}
	for path, want := range map[string]string{"/docs/": "swagger-ui-bundle.js", "/docs/openapi.json": `"openapi"`} {
		w := httptest.NewRecorder()
		api.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest("GET", "/docs", nil))
	if w.Code != 301 || w.Header().Get("Location") != "/docs/" {
		t.Fatal(w.Code, w.Header().Get("Location"))
	}
	// 文档免令牌，接口仍要求令牌。
	w = httptest.NewRecorder()
	api.ServeHTTP(w, httptest.NewRequest("GET", "/v1/status", nil))
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
}

func TestDocsTryItOutSameOrigin(t *testing.T) {
	wb, err := workbench.New(t.TempDir())
	must(t, err)
	defer wb.Close()
	token := strings.Repeat("a", 32)
	api := &API{Workbench: wb, Token: token}
	for origin, want := range map[string]int{"http://example.com": 200, "https://example.com": 200, "https://evil.test": 403, "null": 403} {
		r := httptest.NewRequest("GET", "http://example.com/v1/status", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		api.ServeHTTP(w, r)
		if w.Code != want {
			t.Fatal(origin, w.Code)
		}
	}
}

// 生成请求的字段与 decodeGeneration 接受的字段必须一致：它拒绝未知字段，文档多写或漏写都会误导调用方。
func TestOpenAPIGenerationFields(t *testing.T) {
	var doc openAPIDoc
	must(t, json.Unmarshal(openAPI, &doc))
	want := []string{"text"}
	settings := reflect.TypeOf(schema.SynthesisSettings{})
	for i := range settings.NumField() {
		want = append(want, strings.Split(settings.Field(i).Tag.Get("json"), ",")[0])
	}
	var got []string
	for name := range doc.Components.Schemas["GenerationRequest"].Properties {
		got = append(got, name)
	}
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("GenerationRequest 字段不一致\n文档：%v\n代码：%v", got, want)
	}
}

// 文档中的每个操作都应被路由处理，且返回的状态码已在文档中声明。
func TestOpenAPIRoutes(t *testing.T) {
	var doc openAPIDoc
	must(t, json.Unmarshal(openAPI, &doc))
	wb, err := workbench.New(t.TempDir())
	must(t, err)
	defer wb.Close()
	token := strings.Repeat("a", 32)
	api := &API{Workbench: wb, Token: token}
	path := strings.NewReplacer("{requestId}", strings.Repeat("0", 32), "{id}", "missing")
	for route, operations := range doc.Paths {
		for method, raw := range operations {
			if method == "parameters" {
				continue
			}
			var operation struct {
				Responses map[string]json.RawMessage `json:"responses"`
			}
			must(t, json.Unmarshal(raw, &operation))
			r := httptest.NewRequest(strings.ToUpper(method), path.Replace(route), strings.NewReader("{}"))
			r.Header.Set("Authorization", "Bearer "+token)
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Accept", "application/json, text/event-stream")
			w := httptest.NewRecorder()
			api.ServeHTTP(w, r)
			if w.Code == 405 || operation.Responses[strconv.Itoa(w.Code)] == nil && route != "/mcp" {
				t.Errorf("%s %s 返回未声明的 %d：%s", method, route, w.Code, w.Body.String())
			}
		}
	}
}
