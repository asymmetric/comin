package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nlewo/comin/internal/broker"
	"github.com/nlewo/comin/internal/types"
	"github.com/nlewo/comin/pkg/protobuf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func pinServer(t *testing.T, body *atomic.Value, status int) *httptest.Server {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(ts.Close)
	return ts
}

func TestFetchPin(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{"store path", http.StatusOK, "/nix/store/abc-nixos-system", "/nix/store/abc-nixos-system", false},
		{"trailing newline", http.StatusOK, "/nix/store/abc-nixos-system\n", "/nix/store/abc-nixos-system", false},
		{"not found", http.StatusNotFound, "/nix/store/abc-nixos-system", "", true},
		{"not a store path", http.StatusOK, "<html>", "", true},
		{"two paths", http.StatusOK, "/nix/store/a\n/nix/store/b", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var body atomic.Value
			body.Store(c.body)
			ts := pinServer(t, &body, c.status)
			got, err := fetchPin(context.Background(), ts.Client(), ts.URL)
			if c.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, c.want, got)
		})
	}
}

func TestFetchPinUnreachable(t *testing.T) {
	_, err := fetchPin(context.Background(), &http.Client{Timeout: time.Second}, "http://127.0.0.1:1/pins/x")
	assert.Error(t, err)
}

func nextNiks3Status(t *testing.T, events chan *protobuf.Event) *protobuf.Niks3Status {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		select {
		case e := <-events:
			if fetched := e.GetFetched(); fetched != nil {
				return fetched.GetNiks3Status()
			}
		case <-timeout:
			t.Fatal("no Fetched event received")
		}
	}
}

func TestNiks3FetcherDetectsChanges(t *testing.T) {
	var body atomic.Value
	body.Store("/nix/store/aaa-system")
	ts := pinServer(t, &body, http.StatusOK)

	bk := broker.New()
	bk.Start()
	defer bk.Stop()
	events := bk.Subscribe("test")

	f := NewNiks3Fetcher(types.Niks3Fetcher{
		Remotes: map[string]types.Niks3Remote{"main": {URL: ts.URL}},
	}, bk)
	f.Start(t.Context())

	// The first fetch is an update.
	f.TriggerFetch([]string{"main"})
	s := nextNiks3Status(t, events)
	require.Len(t, s.Remotes, 1)
	r := s.Remotes[0]
	assert.Equal(t, "main", r.Name)
	assert.True(t, r.Fetched.GetValue())
	assert.True(t, r.Updated.GetValue())
	require.NotNil(t, r.Pininfo)
	assert.Equal(t, "main", r.Pininfo.Name)
	assert.Equal(t, "/nix/store/aaa-system", r.Pininfo.StorePath)
	firstUpdate := r.Pininfo.UpdatedAt.AsTime()

	// The same store path is not an update.
	f.TriggerFetch([]string{"main"})
	r = nextNiks3Status(t, events).Remotes[0]
	assert.False(t, r.Updated.GetValue())
	assert.Equal(t, firstUpdate, r.Pininfo.UpdatedAt.AsTime())

	// A new store path is an update.
	body.Store("/nix/store/bbb-system")
	f.TriggerFetch([]string{"main"})
	r = nextNiks3Status(t, events).Remotes[0]
	assert.True(t, r.Updated.GetValue())
	assert.Equal(t, "/nix/store/bbb-system", r.Pininfo.StorePath)
	assert.False(t, r.Pininfo.UpdatedAt.AsTime().Before(firstUpdate))

	// A failed fetch keeps the last known pin.
	body.Store("garbage")
	f.TriggerFetch([]string{"main"})
	r = nextNiks3Status(t, events).Remotes[0]
	assert.False(t, r.Fetched.GetValue())
	assert.NotEmpty(t, r.FetchErrorMsg)
	assert.Equal(t, "/nix/store/bbb-system", r.Pininfo.StorePath)

	state := f.GetState().GetNiks3Status()
	require.NotNil(t, state)
	assert.Equal(t, "/nix/store/bbb-system", state.Remotes[0].Pininfo.StorePath)
}
