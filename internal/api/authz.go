package api

import (
	"context"
	"net/http"
)

// # RequireRoute

// RequireRoute returns middleware that rejects requests whose API key
// permissions do not include route. The route name must be one of:
// "send", "receive", "ask", "admin".
//
// A key with an empty Routes slice is unrestricted and passes all route checks.
func RequireRoute(route string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			perms := permissionsFromCtx(r.Context())
			if perms == nil {
				// APIKeyMiddleware was not applied upstream - misconfiguration.
				writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
				return
			}
			if !routeAllowed(perms, route) {
				writeError(w, http.StatusForbidden, "API key does not allow route: "+route, "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// # Account and room checks

// CheckAccount reports whether the API key in ctx allows the given accountID.
// An empty accountID or a key with no account restrictions is always allowed.
func CheckAccount(ctx context.Context, accountID string) bool {
	if accountID == "" {
		return true
	}
	perms := permissionsFromCtx(ctx)
	if perms == nil || len(perms.Accounts) == 0 {
		return true
	}
	for _, a := range perms.Accounts {
		if a == accountID {
			return true
		}
	}
	return false
}

// CheckRoom reports whether the API key in ctx allows the given roomID.
// An empty roomID or a key with no room restrictions is always allowed.
func CheckRoom(ctx context.Context, roomID string) bool {
	if roomID == "" {
		return true
	}
	perms := permissionsFromCtx(ctx)
	if perms == nil || len(perms.Rooms) == 0 {
		return true
	}
	for _, rm := range perms.Rooms {
		if rm == roomID {
			return true
		}
	}
	return false
}

// routeAllowed checks whether route appears in perms.Routes.
// A nil or empty Routes slice means all routes are allowed.
func routeAllowed(perms *Permissions, route string) bool {
	if len(perms.Routes) == 0 {
		return true
	}
	for _, r := range perms.Routes {
		if r == route {
			return true
		}
	}
	return false
}
