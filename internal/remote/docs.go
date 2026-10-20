package remote

import (
	_ "embed"
	"net/http"
	"net/url"
)

// openAPI 描述 /v1 接口；修改路由或生成参数时同步更新，测试会核对两者一致。
//
//go:embed openapi.json
var openAPI []byte

// docsPage 用 Swagger UI 渲染 openapi.json。资源从 CDN 按固定版本和 SRI 加载，不随服务打包；
// 离线时只影响文档页，接口本身不受影响。
const docsPage = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>yovoice API</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui.css" integrity="sha384-wxLW6kwyHktdDGr6Pv1zgm/VGJh99lfUbzSn6HNHBENZlCN7W602k9VkGdxuFvPn" crossorigin="anonymous">
</head>
<body>
<div id="docs"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5.17.14/swagger-ui-bundle.js" integrity="sha384-wmyclcVGX/WhUkdkATwhaK1X1JtiNrr2EoYJ+diV3vj4v6OC5yCeSu+yW13SYJep" crossorigin="anonymous"></script>
<script>SwaggerUIBundle({ url: 'openapi.json', dom_id: '#docs', deepLinking: true, persistAuthorization: false });</script>
</body>
</html>`

// serveDocs 提供无需令牌的接口文档；文档只描述接口，不含任何数据。
func serveDocs(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != "GET" && r.Method != "HEAD" {
		return false
	}
	switch r.URL.Path {
	case "/docs", "/docs/":
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'unsafe-inline' https://cdn.jsdelivr.net; style-src https://cdn.jsdelivr.net 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'")
		if r.URL.Path == "/docs" {
			// 相对地址 openapi.json 需在 /docs/ 下解析。
			http.Redirect(w, r, "/docs/", http.StatusMovedPermanently)
			return true
		}
		_, _ = w.Write([]byte(docsPage))
	case "/docs/openapi.json":
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(openAPI)
	default:
		return false
	}
	return true
}

// sameOrigin 判断浏览器请求是否来自本服务自己的页面（文档页的“Try it out”）。
// 令牌仍需显式填写，浏览器不会自动携带，因此放行同源不会引入跨站请求。
func sameOrigin(r *http.Request) bool {
	u, err := url.Parse(r.Header.Get("Origin"))
	return err == nil && u.Host != "" && u.Host == r.Host
}
