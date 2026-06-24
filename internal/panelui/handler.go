// Package panelui 提供管理面板前端静态资源。
package panelui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed static/*
var embeddedStatic embed.FS

// Handler 返回 /panel 前端入口处理器；未知子路径回退到 index.html 支持 SPA 路由。
func Handler() http.Handler {
	staticFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		panic(err)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path == "/panel" {
			http.Redirect(w, r, "/panel/", http.StatusFound)
			return
		}

		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/panel/")
		if name == "" || name == "." {
			http.ServeFileFS(w, r, staticFS, "index.html")
			return
		}
		if file, err := staticFS.Open(name); err == nil {
			defer file.Close()
			if stat, err := file.Stat(); err == nil && !stat.IsDir() {
				http.ServeFileFS(w, r, staticFS, name)
				return
			}
		}

		http.ServeFileFS(w, r, staticFS, "index.html")
	})
}
