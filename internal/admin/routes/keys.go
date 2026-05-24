package routes

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/ilamparithi-in/matfix/internal/persistence"
)

// # JSON helpers

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
		Code  string `json:"code,omitempty"`
	}{Error: message, Code: code})
}

// # Request / response types

// KeyPermissions mirrors api.Permissions - same JSON shape so the stored
// permissions_json is readable by both the API auth middleware and the admin
// routes without coupling the two packages.
type KeyPermissions struct {
	Accounts     []string `json:"accounts,omitempty"`
	Routes       []string `json:"routes,omitempty"`
	Rooms        []string `json:"rooms,omitempty"`
	EventTypes   []string `json:"event_types,omitempty"`
	RateLimitRPS int      `json:"rate_limit_rps,omitempty"`
}

type createKeyRequest struct {
	Name        string          `json:"name"`
	Permissions *KeyPermissions `json:"permissions,omitempty"`
}

// keyCreatedResponse is returned when a key is created or rotated.
// The Key field contains the plaintext key exactly once; it is never stored.
type keyCreatedResponse struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	Name      string `json:"name"`
	CreatedAt int64  `json:"created_at"`
}

type keyListEntry struct {
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Permissions *KeyPermissions `json:"permissions,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	RevokedAt   *int64          `json:"revoked_at,omitempty"`
}

type listKeysResponse struct {
	Keys []keyListEntry `json:"keys"`
}

// # Key generation

// generateKey creates a cryptographically random 32-byte key, hex-encodes it
// as the plaintext, and returns the SHA-256 hash of that plaintext for storage.
// The plaintext is never written to any persistent store.
func generateKey() (plaintext, hash string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(raw)
	h := sha256.Sum256([]byte(plaintext))
	hash = hex.EncodeToString(h[:])
	return plaintext, hash, nil
}

// marshalPermissions converts KeyPermissions to its JSON representation for
// storage. Returns an empty string when p is nil (unrestricted key).
func marshalPermissions(p *KeyPermissions) (string, error) {
	if p == nil {
		return "", nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// # Handlers

// CreateKeyHandler handles POST /keys.
//
// It generates a cryptographically random key, stores SHA-256(key) with the
// supplied metadata, and returns the plaintext key exactly once. The plaintext
// is never persisted.
func CreateKeyHandler(store persistence.APIKeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body", "bad_request")
			return
		}
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required", "bad_request")
			return
		}

		plaintext, hash, err := generateKey()
		if err != nil {
			slog.Error("admin: key generation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "key generation failed", "internal_error")
			return
		}

		permsJSON, err := marshalPermissions(req.Permissions)
		if err != nil {
			slog.Error("admin: marshal permissions failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error", "internal_error")
			return
		}

		now := time.Now().UnixMilli()
		row := persistence.APIKeyRow{
			ID:              uuid.New().String(),
			KeyHash:         hash,
			Name:            req.Name,
			PermissionsJSON: permsJSON,
			CreatedAt:       now,
		}
		if err := store.Insert(r.Context(), row); err != nil {
			slog.Error("admin: insert key failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to store key", "internal_error")
			return
		}

		writeJSON(w, http.StatusCreated, keyCreatedResponse{
			ID:        row.ID,
			Key:       plaintext,
			Name:      row.Name,
			CreatedAt: row.CreatedAt,
		})
	}
}

// ListKeysHandler handles GET /keys.
//
// It returns all key records. Key values are never included in the response.
func ListKeysHandler(store persistence.APIKeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := store.List(r.Context())
		if err != nil {
			slog.Error("admin: list keys failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to list keys", "internal_error")
			return
		}

		entries := make([]keyListEntry, 0, len(rows))
		for _, row := range rows {
			entry := keyListEntry{
				ID:        row.ID,
				Name:      row.Name,
				CreatedAt: row.CreatedAt,
				RevokedAt: row.RevokedAt,
			}
			if row.PermissionsJSON != "" {
				var p KeyPermissions
				if err := json.Unmarshal([]byte(row.PermissionsJSON), &p); err == nil {
					entry.Permissions = &p
				}
			}
			entries = append(entries, entry)
		}

		writeJSON(w, http.StatusOK, listKeysResponse{Keys: entries})
	}
}

// RevokeKeyHandler handles DELETE /keys/{id}.
//
// It soft-revokes the key by recording a revocation timestamp. The record is
// retained for audit purposes; the key will be rejected by the auth middleware.
func RevokeKeyHandler(store persistence.APIKeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "missing key id", "bad_request")
			return
		}

		if err := store.SetRevoked(r.Context(), id, time.Now().UnixMilli()); err != nil {
			slog.Error("admin: revoke key failed", "key_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to revoke key", "internal_error")
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// RotateKeyHandler handles POST /keys/{id}/rotate.
//
// It revokes the identified key and creates a replacement with the same name
// and permissions. The new plaintext key is returned exactly once.
//
// The operation is performed as two sequential writes (revoke → insert). If the
// insert fails after revoke, the caller may create a fresh key manually. The
// original key is considered revoked regardless.
func RotateKeyHandler(store persistence.APIKeyStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		if id == "" {
			writeError(w, http.StatusBadRequest, "missing key id", "bad_request")
			return
		}

		existing, err := store.GetByID(r.Context(), id)
		if err != nil {
			slog.Error("admin: rotate key lookup failed", "key_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to look up key", "internal_error")
			return
		}
		if existing == nil {
			writeError(w, http.StatusNotFound, "key not found", "not_found")
			return
		}

		// Revoke the existing key first.
		if err := store.SetRevoked(r.Context(), id, time.Now().UnixMilli()); err != nil {
			slog.Error("admin: rotate revoke failed", "key_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to revoke existing key", "internal_error")
			return
		}

		// Generate and insert the replacement key.
		plaintext, hash, err := generateKey()
		if err != nil {
			slog.Error("admin: rotate key generation failed", "error", err)
			writeError(w, http.StatusInternalServerError, "key generation failed", "internal_error")
			return
		}

		now := time.Now().UnixMilli()
		newRow := persistence.APIKeyRow{
			ID:              uuid.New().String(),
			KeyHash:         hash,
			Name:            existing.Name,
			PermissionsJSON: existing.PermissionsJSON,
			CreatedAt:       now,
		}
		if err := store.Insert(r.Context(), newRow); err != nil {
			slog.Error("admin: rotate insert replacement key failed", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to create replacement key", "internal_error")
			return
		}

		writeJSON(w, http.StatusCreated, keyCreatedResponse{
			ID:        newRow.ID,
			Key:       plaintext,
			Name:      newRow.Name,
			CreatedAt: newRow.CreatedAt,
		})
	}
}
