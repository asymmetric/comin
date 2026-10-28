package fetcher

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nlewo/comin/internal/broker"
	"github.com/nlewo/comin/internal/types"
	"github.com/nlewo/comin/pkg/protobuf"
	"github.com/stretchr/testify/assert"
)

func TestFetch(t *testing.T) {
	// Create a test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("/nix/store/path\n")) // nolint: errcheck
	}))
	defer ts.Close()

	storePath, err := fetch(ts.URL)
	assert.NoError(t, err)
	assert.Equal(t, "/nix/store/path", storePath)
}

func TestFetchInvalidStorePath(t *testing.T) {
	for _, body := range []string{"<html>", "/nix/store/a\n/nix/store/b"} {
		// Create a test server that doesn't return a store path
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(body)) // nolint: errcheck
		}))
		defer ts.Close()

		storePath, err := fetch(ts.URL)
		assert.Error(t, err)
		assert.Empty(t, storePath)
	}
}

func TestFetchServerError(t *testing.T) {
	// Create a test server that returns 500
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	storePath, err := fetch(ts.URL)
	assert.Error(t, err)
	assert.Empty(t, storePath)
}

func TestFetchInvalidURL(t *testing.T) {
	storePath, err := fetch("http://invalid-url-that-does-not-exist")
	assert.Error(t, err)
	assert.Empty(t, storePath)
}

func TestNiks3FetcherStart(t *testing.T) {
	// Create a test server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("/nix/store/path")) // nolint: errcheck
	}))
	defer ts.Close()

	// Create broker and fetcher
	bk := broker.New()
	bk.Start()
	defer bk.Stop()

	config := types.Niks3Fetcher{
		Remotes: map[string]types.Niks3Remote{
			"test-remote": {URL: ts.URL},
		},
	}

	f := NewNiks3Fetcher(config, bk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)

	// Subscribe to broker events
	brokerEvents := bk.Subscribe()

	// Trigger fetch
	f.TriggerFetch([]string{"test-remote"})

	// Wait for the Fetched event
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		e := <-brokerEvents
		fetched := e.GetFetched()
		assert.NotNil(c, fetched)
		assert.True(c, fetched.Updated)
		assert.NotNil(c, fetched.Type)
		niks3Status := fetched.Type.(*protobuf.Event_Fetched_Niks3Status)
		assert.NotNil(c, niks3Status)
		assert.NotNil(c, niks3Status.Niks3Status)
		assert.Len(c, niks3Status.Niks3Status.Remotes, 1)
		assert.Equal(c, "test-remote", niks3Status.Niks3Status.Remotes[0].Name)
		assert.True(c, niks3Status.Niks3Status.Remotes[0].Fetched.GetValue())
		// Verify PinInfo is set
		assert.NotNil(c, niks3Status.Niks3Status.Remotes[0].Pininfo)
		assert.Equal(c, "test-remote", niks3Status.Niks3Status.Remotes[0].Pininfo.Name)
		assert.Equal(c, "/nix/store/path", niks3Status.Niks3Status.Remotes[0].Pininfo.StorePath)
		assert.NotNil(c, niks3Status.Niks3Status.Remotes[0].Pininfo.CreatedAt)
		assert.NotNil(c, niks3Status.Niks3Status.Remotes[0].Pininfo.UpdatedAt)
		// Verify FetchedAt timestamp is set
		assert.NotNil(c, niks3Status.Niks3Status.Remotes[0].FetchedAt)
	}, 5*time.Second, 100*time.Millisecond, "expected Fetched event not received")

	// Verify the fetcher state
	state := f.GetState()
	assert.True(t, state.IsFetching.GetValue() == false)
	niks3Status := state.Status.(*protobuf.Fetcher_Niks3Status)
	assert.NotNil(t, niks3Status)
	assert.Len(t, niks3Status.Niks3Status.Remotes, 1)
	assert.Equal(t, "test-remote", niks3Status.Niks3Status.Remotes[0].Name)
	assert.True(t, niks3Status.Niks3Status.Remotes[0].Fetched.GetValue())
	// Verify PinInfo is set in state
	assert.NotNil(t, niks3Status.Niks3Status.Remotes[0].Pininfo)
	assert.Equal(t, "test-remote", niks3Status.Niks3Status.Remotes[0].Pininfo.Name)
	assert.Equal(t, "/nix/store/path", niks3Status.Niks3Status.Remotes[0].Pininfo.StorePath)
	assert.NotNil(t, niks3Status.Niks3Status.Remotes[0].Pininfo.CreatedAt)
	assert.NotNil(t, niks3Status.Niks3Status.Remotes[0].Pininfo.UpdatedAt)
	// Verify FetchedAt timestamp is set
	assert.NotNil(t, niks3Status.Niks3Status.Remotes[0].FetchedAt)
}

func TestNiks3FetcherUpdatedComparison(t *testing.T) {
	// Create a test server with a store path
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("/nix/store/path")) // nolint: errcheck
	}))
	defer ts.Close()

	// Create another test server with the same store path
	sameTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("/nix/store/path")) // nolint: errcheck
	}))
	defer sameTs.Close()

	// Create a test server with another store path
	otherTs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("/nix/store/other-path")) // nolint: errcheck
	}))
	defer otherTs.Close()

	// Create broker and fetcher
	bk := broker.New()
	bk.Start()
	defer bk.Stop()

	config := types.Niks3Fetcher{
		Remotes: map[string]types.Niks3Remote{
			"test-remote": {URL: ts.URL},
		},
	}

	f := NewNiks3Fetcher(config, bk)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.Start(ctx)

	// Subscribe to broker events
	brokerEvents := bk.Subscribe()

	// Trigger first fetch
	f.TriggerFetch([]string{"test-remote"})

	// Wait for the Fetched event and verify Updated is true
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		e := <-brokerEvents
		fetched := e.GetFetched()
		assert.NotNil(c, fetched)
		assert.True(c, fetched.Updated)
	}, 5*time.Second, 100*time.Millisecond, "expected Fetched event with Updated=true")

	// Verify the remote has Updated=true
	state := f.GetState()
	niks3Status := state.Status.(*protobuf.Fetcher_Niks3Status)
	assert.NotNil(t, niks3Status.Niks3Status.Remotes[0].Updated)
	assert.True(t, niks3Status.Niks3Status.Remotes[0].Updated.GetValue())

	// Update config to point to the server with the same store path
	config.Remotes["test-remote"] = types.Niks3Remote{URL: sameTs.URL}
	f.config = config

	// Trigger second fetch with the same store path
	f.TriggerFetch([]string{"test-remote"})

	// Wait for the Fetched event and verify Updated is false
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		e := <-brokerEvents
		fetched := e.GetFetched()
		assert.NotNil(c, fetched)
		assert.False(c, fetched.Updated)
	}, 5*time.Second, 100*time.Millisecond, "expected Fetched event with Updated=false")

	// Verify the remote has Updated=false (same store path)
	state = f.GetState()
	niks3Status = state.Status.(*protobuf.Fetcher_Niks3Status)
	assert.NotNil(t, niks3Status.Niks3Status.Remotes[0].Updated)
	assert.False(t, niks3Status.Niks3Status.Remotes[0].Updated.GetValue())

	// Update config to point to the server with another store path
	config.Remotes["test-remote"] = types.Niks3Remote{URL: otherTs.URL}
	f.config = config

	// Trigger third fetch with another store path
	f.TriggerFetch([]string{"test-remote"})

	// Wait for the Fetched event and verify Updated is true
	assert.EventuallyWithT(t, func(c *assert.CollectT) {
		e := <-brokerEvents
		fetched := e.GetFetched()
		assert.NotNil(c, fetched)
		assert.True(c, fetched.Updated)
	}, 5*time.Second, 100*time.Millisecond, "expected Fetched event with Updated=true")

	// Verify the remote has the new store path
	state = f.GetState()
	niks3Status = state.Status.(*protobuf.Fetcher_Niks3Status)
	assert.True(t, niks3Status.Niks3Status.Remotes[0].Updated.GetValue())
	assert.Equal(t, "/nix/store/other-path", niks3Status.Niks3Status.Remotes[0].Pininfo.StorePath)
}
