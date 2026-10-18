package admin

import (
	"net/http"
	"strings"

	"opencode2api/internal/config"
	"opencode2api/internal/httpx"
)

// Parsing does not write or apply configuration. The operator can review all
// imported entries before the normal atomic configuration save.
func (a *Server) handleParseProxies(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var input struct {
		Files []struct {
			Name    string `json:"name"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := decodeAdminJSON(w, r, &input); err != nil {
		writeAdminError(w, http.StatusBadRequest, "invalid_request", "无法读取代理文件；请求总大小上限 2 MiB")
		return
	}
	if len(input.Files) == 0 || len(input.Files) > 32 {
		writeAdminError(w, http.StatusBadRequest, "invalid_files", "请选择 1–32 个代理文件")
		return
	}
	seen := make(map[string]bool)
	entries := make([]string, 0)
	for _, file := range input.Files {
		proxies, err := config.ParseProxyContent(file.Name, strings.NewReader(file.Content))
		if err != nil {
			writeAdminError(w, http.StatusBadRequest, "invalid_proxy_file", "代理文件解析失败："+err.Error())
			return
		}
		for _, proxy := range proxies {
			clean, pool := config.ParseProxyEntry(proxy)
			if pool {
				clean += "#pool"
			}
			if !seen[clean] {
				seen[clean] = true
				entries = append(entries, proxy)
			}
		}
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"proxies": entries, "files": len(input.Files), "count": len(entries)})
}
