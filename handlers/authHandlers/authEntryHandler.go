
package authHandlers

import (
	"log"
	"net/http"
	"strings"

	management "github.com/satyamraj1643/pine_backend_v2/handlers/authHandlers/accountManagementHandlers"
	coreactions "github.com/satyamraj1643/pine_backend_v2/handlers/authHandlers/coreActionsHandlers"
	security "github.com/satyamraj1643/pine_backend_v2/handlers/authHandlers/securityHandlers"
	social "github.com/satyamraj1643/pine_backend_v2/handlers/authHandlers/socialLoginHandlers"
)

func AuthHandler(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path

	log.Println("+ Auth Path: ", path)

	switch {
	case strings.HasPrefix(path, "/auth/core"):
		coreactions.HandleCoreActions(w, r)

	case strings.HasPrefix(path, "/auth/social"):
		social.HandleSocial(w, r)

	case strings.HasPrefix(path, "/auth/security"):
		security.HandleSecurity(w, r)

	case strings.HasPrefix(path, "/auth/management"):
		management.HandleAccountManagement(w, r)

	default:
		http.Error(w, "Auth route not found", http.StatusNotFound)
		w.Write([]byte("Auth route not found"))
	}
}
