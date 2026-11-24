package fetcher

import (
	"context"
	"fmt"
	"io"
	"net/http"
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

func NewNiks3Fetcher(config types.Niks3Fetcher, broker *broker.Broker) *Niks3Fetcher {
	f := &Niks3Fetcher{
		config:        config,
		broker:        broker,
		submitRemotes: make(chan []string),
	}
	for k, v := range config.Remotes {
		f.remotes = append(f.remotes, &protobuf.Niks3Remote{
			Name: k,
			Url:  v.URL,
		})
	}
	return f
}

type Niks3Fetcher struct {
	config        types.Niks3Fetcher
	isFetching    atomic.Bool
	mu            sync.RWMutex
	submitRemotes chan []string
	broker        *broker.Broker
	remotes       []*protobuf.Niks3Remote
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

// status returns a copy of the remotes, which can be safely sent to
// other goroutines.
func (f *Niks3Fetcher) status() *protobuf.Niks3Status {
	f.mu.RLock()
	defer f.mu.RUnlock()
	s := &protobuf.Niks3Status{}
	for _, r := range f.remotes {
		s.Remotes = append(s.Remotes, proto.Clone(r).(*protobuf.Niks3Remote))
	}
	return s
}

// fetch returns the store path of a pin. niks3 serves a pin as a plain
// text store path at <cache-url>/pins/<name>.
func fetch(route string) (string, error) {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Get(route)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() // nolint: errcheck

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	storePath := strings.TrimSpace(string(body))
	if !strings.HasPrefix(storePath, "/nix/store/") || strings.ContainsAny(storePath, " \t\r\n") {
		return "", fmt.Errorf("the pin content '%.100s' is not a store path", storePath)
	}
	return storePath, nil
}

func (f *Niks3Fetcher) Start(ctx context.Context) {
	logrus.Info("fetcher niks3: starting")
	go func() {
		for {
			updated := false
			submittedRemotes := <-f.submitRemotes
			logrus.Debugf("niks3 fetcher: remotes submitted: %v", submittedRemotes)
			f.isFetching.Store(true)
			for _, remoteName := range submittedRemotes {
				storePath, err := fetch(f.config.Remotes[remoteName].URL)
				errMsg := ""
				if err != nil {
					logrus.Errorf("niks3 fetcher: failed to fetch remote %s: %v", remoteName, err)
					errMsg = err.Error()
				}
				f.mu.Lock()
				for _, r := range f.remotes {
					if r.Name == remoteName {
						r.FetchErrorMsg = errMsg
						r.Fetched = wrapperspb.Bool(err == nil)
						r.FetchedAt = timestamppb.New(time.Now().UTC())
						r.Updated = wrapperspb.Bool(false)
						// The pin is updated when its store path changes
						if err == nil && (r.Pininfo == nil || r.Pininfo.StorePath != storePath) {
							now := timestamppb.New(time.Now().UTC())
							r.Pininfo = &protobuf.PinInfo{
								Name:      remoteName,
								StorePath: storePath,
								CreatedAt: now,
								UpdatedAt: now,
							}
							r.Updated = wrapperspb.Bool(true)
							updated = true
						}
					}
				}
				f.mu.Unlock()
				if err == nil {
					logrus.Debugf("niks3 fetcher: fetched remote %s, pin: %s", remoteName, storePath)
				}
			}
			f.isFetching.Store(false)
			// Publish fetched event
			f.broker.Publish(&protobuf.Event{
				Type: &protobuf.Event_Fetched_{Fetched: &protobuf.Event_Fetched{
					Type: &protobuf.Event_Fetched_Niks3Status{
						Niks3Status: f.status(),
					},
					Updated:  updated,
					Verified: false,
				}},
				CreatedAt: timestamppb.New(time.Now().UTC()),
			})
		}
	}()
}
