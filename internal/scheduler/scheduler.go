package scheduler

import (
	"fmt"
	"time"

	"github.com/go-co-op/gocron/v2"
	"github.com/nlewo/comin/internal/fetcher"
	"github.com/nlewo/comin/internal/types"
	"github.com/sirupsen/logrus"
)

type Scheduler struct {
	s gocron.Scheduler
}

func New() Scheduler {
	s, _ := gocron.NewScheduler()

	sched := Scheduler{
		s: s,
	}
	go sched.s.Start()
	return sched
}

func (s Scheduler) FetchRemotes(fetcher fetcher.Fetcher, remotes []types.Remote) {
	for _, remote := range remotes {
		s.fetchRemote(fetcher, remote.Name, remote.Poller.Period)
	}
}

func (s Scheduler) FetchNiks3Remotes(fetcher fetcher.Fetcher, remotes map[string]types.Niks3Remote) {
	for name, remote := range remotes {
		s.fetchRemote(fetcher, name, remote.Poller.Period)
	}
}

func (s Scheduler) fetchRemote(fetcher fetcher.Fetcher, name string, period int) {
	if period == 0 {
		return
	}
	logrus.Infof("scheduler: starting the period job for the remote '%s' with period %ds", name, period)
	_, _ = s.s.NewJob(
		gocron.DurationJob(
			time.Duration(period)*time.Second,
		),
		gocron.NewTask(
			func() {
				logrus.Debugf("scheduler: running task for remote %s", name)
				fetcher.TriggerFetch([]string{name})
			},
		),
		gocron.WithSingletonMode(gocron.LimitModeReschedule),
		gocron.WithName(fmt.Sprintf("fetch-remote-%s", name)),
	)
}
