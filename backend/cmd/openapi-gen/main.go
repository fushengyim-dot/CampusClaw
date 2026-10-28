package main

import (
	"encoding/json"
	"flag"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type operation struct {
	Summary     string           `json:"summary"`
	Parameters  []map[string]any `json:"parameters,omitempty"`
	RequestBody map[string]any   `json:"requestBody,omitempty"`
	Responses   map[string]any   `json:"responses"`
}
type spec struct {
	OpenAPI    string                          `json:"openapi"`
	Info       map[string]string               `json:"info"`
	Paths      map[string]map[string]operation `json:"paths"`
	Components map[string]any                  `json:"components"`
	Security   []map[string][]string           `json:"security"`
}

func main() {
	out := flag.String("out", "openapi.json", "output JSON")
	flag.Parse()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	must(err)
	doc := spec{OpenAPI: "3.0.3", Info: map[string]string{"title": "CampusClaw API", "version": "0.5.0", "description": "从Go处理器注解生成的接口清单。所有业务接口使用HttpOnly登录Cookie。"}, Paths: map[string]map[string]operation{}, Components: map[string]any{"securitySchemes": map[string]any{"sessionCookie": map[string]string{"type": "apiKey", "in": "cookie", "name": "campusclaw_session"}}}, Security: []map[string][]string{{"sessionCookie": {}}}}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || e.Name() == "openapi_generated.go" {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, parser.ParseComments)
		must(err)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Doc == nil {
				continue
			}
			for _, c := range fn.Doc.List {
				line := strings.TrimSpace(strings.TrimPrefix(c.Text, "//"))
				if !strings.HasPrefix(line, "@api ") {
					continue
				}
				fields := strings.SplitN(strings.TrimPrefix(line, "@api "), " ", 3)
				if len(fields) < 3 {
					continue
				}
				method, path := strings.ToLower(fields[0]), fields[1]
				summary := fields[2]
				if doc.Paths[path] == nil {
					doc.Paths[path] = map[string]operation{}
				}
				op := operation{Summary: summary, Responses: map[string]any{"200": map[string]string{"description": "请求成功"}, "400": map[string]string{"description": "输入无效"}, "401": map[string]string{"description": "未登录"}, "403": map[string]string{"description": "权限不足"}, "404": map[string]string{"description": "资源不存在"}, "503": map[string]string{"description": "服务暂不可用"}}}
				if strings.Contains(path, "{id}") {
					op.Parameters = []map[string]any{{"name": "id", "in": "path", "required": true, "schema": map[string]string{"type": "integer", "format": "int64"}}}
				}
				if method == "post" || method == "put" {
					op.RequestBody = map[string]any{"required": true, "content": map[string]any{"application/json": map[string]any{"schema": map[string]any{"type": "object", "additionalProperties": true}}}}
				}
				doc.Paths[path][method] = op
			}
		}
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	must(err)
	must(os.WriteFile(filepath.Clean(*out), append(data, '\n'), 0600))
}
func must(err error) {
	if err != nil {
		panic(err)
	}
}
