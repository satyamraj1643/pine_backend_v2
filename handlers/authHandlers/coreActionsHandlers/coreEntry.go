package coreactions

import (
	"net/http"
)



func HandleCoreActions(w http.ResponseWriter, r *http.Request) {
    path := r.URL.Path

	switch {
	case path == "/auth/core/signup":
		HandleSignup(w, r)
	case path == "/auth/core/login":
		HandleLogin(w, r)
	case path == "/auth/core/logout":
		HandleLogout(w, r)
	case path == "/auth/core/delete":
		HandleDelete(w, r)
	case path == "/auth/core/deactivate":
		HandleDeactivate(w, r)
	case path == "/auth/core/archive":
		HandleArchive(w, r)
	case path == "/auth/core/update":
		HandleUpdate(w, r)
	default:
		http.Error(w, "Auth route not found", http.StatusNotFound)
		w.Write([]byte("Auth route not found"))
	}
}
