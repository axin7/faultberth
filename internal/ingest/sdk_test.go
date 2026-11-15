package ingest

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

func TestSentryGoSDK(t *testing.T) {
	store := &fakeStore{key: "publickey"}
	server := httptest.NewServer(Handler{store})
	defer server.Close()
	dsn := strings.Replace(server.URL, "http://", "http://publickey@", 1) + "/2"
	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: dsn})
	if err != nil {
		t.Fatal(err)
	}
	id := client.CaptureMessage("synthetic sdk integration test", nil, nil)
	if id == nil || !client.Flush(5*time.Second) {
		t.Fatal("SDK did not send event")
	}
	if store.id != string(*id) || !strings.Contains(string(store.saved), "synthetic sdk integration test") {
		t.Fatalf("SDK event not stored: id=%s", store.id)
	}
}
