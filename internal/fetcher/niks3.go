package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nlewo/comin/internal/broker"
	"github.com/nlewo/comin/internal/types"
	"github.com/nlewo/comin/pkg/protobuf"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// maxPinSize bounds the response body: a pin is a single store path.
const maxPinSize = 4096

func NewNiks3Fetcher(config types.Niks3Fetcher, broker *broker.Broker) *Niks3Fetcher {
	f := &Niks3Fetcher{
		config:        config,
		broker:        broker,
		submitRemotes: make(chan []string),
		client:        &http.Client{Timeout: 10 * time.Second},
	}
	names := make([]string, 0, len(config.Remotes))
	for name := range config.Remotes {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		f.remotes = append(f.remotes, &protobuf.Niks3Remote{
			Name: name,
			Url:  config.Remotes[name].URL,
		})
	}
	return f
}

// Niks3Fetcher polls niks3 pins. niks3 stores each pin in the bucket at
// pins/<name> as a plain-text store path, which the niks3 read proxy
// serves at <cache-url>/pins/<name>.
type Niks3Fetcher struct {
	config        types.Niks3Fetcher
	isFetching    atomic.Bool
	mu            sync.RWMutex
	submitRemotes chan []string
	broker        *broker.Broker
	remotes       []*protobuf.Niks3Remote
	client        *http.Client
}

func (f *Niks3Fetcher) IsFetching() bool {
	return f.isFetching.Load()
}

func (f *Niks3Fetcher) TriggerFetch(remotes []string) {
	f.submitRemotes <- remotes
}

func (f *Niks3Fetcher) GetState() *protobuf.Fetcher {
	return &protobuf.Fetcher{
		IsFetching: wrapperspb.Bool(f.isFetching.Load()),
		Status: &protobuf.Fetcher_Niks3Status{
			Niks3Status: f.status(),
		},
	}
}

// status returns a deep copy of the remotes, safe to hand to other goroutines.
func (f *Niks3Fetcher) status() *protobuf.Niks3Status {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s := &protobuf.Niks3Status{}
	for _, r := range f.remotes {
		s.Remotes = append(s.Remotes, proto.Clone(r).(*protobuf.Niks3Remote))
	}
	return s
}

// fetchPin returns the store path a pin URL points to.
func fetchPin(ctx context.Context, client *http.Client, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() // nolint

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPinSize+1))
	if err != nil {
		return "", err
	}
	if len(body) > maxPinSize {
		return "", fmt.Errorf("the pin is larger than %d bytes", maxPinSize)
	}
	storePath := strings.TrimSpace(string(body))
	if !strings.HasPrefix(storePath, "/nix/store/") || strings.ContainsAny(storePath, " \t\r\n") {
		return "", fmt.Errorf("the pin content '%.100s' is not a store path", storePath)
	}
	return storePath, nil
}

// update records the result of a fetch. A pin is considered updated when
// its store path differs from the previously fetched one; its UpdatedAt is
// then set to the fetch time.
func (f *Niks3Fetcher) update(name, storePath string, fetchErr error, now time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, r := range f.remotes {
		if r.Name != name {
			continue
		}
		r.FetchedAt = timestamppb.New(now)
		r.Fetched = wrapperspb.Bool(fetchErr == nil)
		r.Updated = wrapperspb.Bool(false)
		if fetchErr != nil {
			r.FetchErrorMsg = fetchErr.Error()
			return
		}
		r.FetchErrorMsg = ""
		if r.Pininfo == nil || r.Pininfo.StorePath != storePath {
			r.Updated = wrapperspb.Bool(true)
			r.Pininfo = &protobuf.PinInfo{
				Name:      name,
				StorePath: storePath,
				CreatedAt: timestamppb.New(now),
				UpdatedAt: timestamppb.New(now),
			}
		}
		return
	}
}

func (f *Niks3Fetcher) Start(ctx context.Context) {
	logrus.Info("fetcher niks3: starting")
	go func() {
		for {
			var submittedRemotes []string
			select {
			case <-ctx.Done():
				return
			case submittedRemotes = <-f.submitRemotes:
			}
			logrus.Debugf("fetcher niks3: remotes submitted: %v", submittedRemotes)
			f.isFetching.Store(true)
			for _, name := range submittedRemotes {
				remote, ok := f.config.Remotes[name]
				if !ok {
					logrus.Errorf("fetcher niks3: unknown remote %s", name)
					continue
				}
				storePath, err := fetchPin(ctx, f.client, remote.URL)
				if err != nil {
					logrus.Errorf("fetcher niks3: failed to fetch remote %s: %v", name, err)
				} else {
					logrus.Debugf("fetcher niks3: remote %s pins %s", name, storePath)
				}
				f.update(name, storePath, err, time.Now().UTC())
			}
			f.isFetching.Store(false)
			f.broker.Publish(&protobuf.Event{
				Type: &protobuf.Event_Fetched_{Fetched: &protobuf.Event_Fetched{
					Type: &protobuf.Event_Fetched_Niks3Status{
						Niks3Status: f.status(),
					},
					// Pins are not signed: the Nix daemon checks the
					// narinfo signatures when it substitutes the closure.
					Verified: true,
				}},
				CreatedAt: timestamppb.New(time.Now().UTC()),
			})
		}
	}()
}
