package scheduler

import (
	"time"

	"github.com/alde/eremetic/database"
	"github.com/alde/eremetic/types"
	log "github.com/dmuth/google-go-log4go"
	"github.com/gogo/protobuf/proto"
	mesos "github.com/mesos/mesos-go/mesosproto"
	sched "github.com/mesos/mesos-go/scheduler"
)

var (
	maxReconciliationDelay = 120
)

type Reconcile struct {
	cancel chan struct{}
}

func (r *Reconcile) Cancel() {
	close(r.cancel)
}

func ReconcileTasks(driver sched.SchedulerDriver) *Reconcile {
	cancel := make(chan struct{})

	go func() {
		var (
			c     uint
			delay int
		)

		tasks, err := database.ListNonTerminalTasks()
		if err != nil {
			log.Errorf("Failed to list non-terminal tasks: %s", err)
			return
		}

		log.Infof("Trying to reconcile with %d task(s)", len(tasks))
		start := time.Now()

		for len(tasks) > 0 {
			select {
			case <-cancel:
				log.Info("Cancelling reconciliation job")
				return
			case <-time.After(time.Duration(delay) * time.Second):
				// Filter tasks that has received a status update
				ntasks := []*types.EremeticTask{}
				for _, t := range tasks {
					nt, err := database.ReadTask(t.ID)
					if err != nil {
						log.Warnf("Task %s not found in database", t.ID)
						continue
					}
					if nt.LastUpdated().Before(start) {
						ntasks = append(ntasks, &nt)
					}
				}
				tasks = ntasks

				// Send reconciliation request
				if len(tasks) > 0 {
					var statuses []*mesos.TaskStatus
					for _, t := range tasks {
						statuses = append(statuses, &mesos.TaskStatus{
							State:   mesos.TaskState_TASK_STAGING.Enum(),
							TaskId:  &mesos.TaskID{Value: proto.String(t.ID)},
							SlaveId: &mesos.SlaveID{Value: proto.String(t.SlaveId)},
						})
					}
					log.Debugf("Sending reconciliation request #%d", c)
					driver.ReconcileTasks(statuses)
				}

				if delay < maxReconciliationDelay {
					delay = 10 << c
					if delay >= maxReconciliationDelay {
						delay = maxReconciliationDelay
					}
				}

				c += 1
			}
		}
		log.Info("Reconciliation done")
	}()

	return &Reconcile{
		cancel: cancel,
	}
}
