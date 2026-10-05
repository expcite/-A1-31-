package console

import (
	"io/fs"
	"net/http"
)

func (s *Server) history(w http.ResponseWriter) {
	b, e := fs.ReadFile(assets, "assets/data/m5_history.json")
	if e != nil {
		http.Error(w, "历史数据资源缺失", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(b)
}
