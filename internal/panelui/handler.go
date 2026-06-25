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

// allowedAssets 是允许直接对外返回的静态文件白名单；
// 任何不在白名单内的路径回退到 index.html，避免将来新增的敏感文件被原样返回。
var allowedAssets = map[string]struct{}{
	"index.html": {},
	"app.js":     {},
	"styles.css": {},
}

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
		// 仅对白名单文件走静态文件服务；其余一律回退到 index.html（SPA 路由），
		// 防止 embed FS 内的其他文件（如未来加入的配置）被直接读取。
		if _, ok := allowedAssets[name]; ok {
			if file, err := staticFS.Open(name); err == nil {
				defer file.Close()
				if stat, err := file.Stat(); err == nil && !stat.IsDir() {
					http.ServeFileFS(w, r, staticFS, name)
					return
				}
			}
		}

		http.ServeFileFS(w, r, staticFS, "index.html")
	})
}
