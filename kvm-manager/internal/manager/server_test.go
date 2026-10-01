package manager

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestServerSQLiteAndHealthEndpoints(t *testing.T) {
	srv, err := NewServer(Config{DatabasePath: filepath.Join(t.TempDir(), "manager.db")})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.store.close()

	mux := http.NewServeMux()
	srv.routes(mux)
	for _, test := range []struct {
		path string
		want int
	}{
		{path: "/healthz", want: http.StatusOK},
		{path: "/api/v1/agents", want: http.StatusOK},
		{path: "/api/v1/vms", want: http.StatusOK},
	} {
		req := httptest.NewRequest(http.MethodGet, test.path, nil)
		res := httptest.NewRecorder()
		mux.ServeHTTP(res, req)
		if res.Code != test.want {
			t.Fatalf("GET %s returned %d, want %d", test.path, res.Code, test.want)
		}
	}
}
