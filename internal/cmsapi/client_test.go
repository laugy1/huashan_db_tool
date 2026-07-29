package cmsapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoginAndUpload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dcsn/login":
			if r.Method != http.MethodPost {
				t.Fatalf("login method %s", r.Method)
			}
			_ = r.ParseForm()
			if r.Form.Get("username") != "dev" || r.Form.Get("password") != "secret" {
				http.Error(w, "bad creds", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "test-token-123"})
		case "/dcsn/media/upload":
			if r.Header.Get("Authorization") != "Bearer test-token-123" {
				http.Error(w, "no auth", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"message": "ok",
				"metaList": []map[string]any{
					{"_id": "a.jpg", "path": "https://cdn/a.jpg"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL+"/dcsn", "dev", "secret")
	if err := c.Login(context.Background()); err != nil {
		t.Fatal(err)
	}
	meta, err := c.Upload(context.Background(), "a.jpg", []byte("fake"))
	if err != nil {
		t.Fatal(err)
	}
	if meta[0]["_id"] != "a.jpg" {
		t.Fatalf("meta _id = %v", meta[0]["_id"])
	}
}

func TestExtractToken_nested(t *testing.T) {
	body := []byte(`{"data":{"token":"nested"}}`)
	tok, err := extractToken(body)
	if err != nil || tok != "nested" {
		t.Fatalf("got %q err %v", tok, err)
	}
}
