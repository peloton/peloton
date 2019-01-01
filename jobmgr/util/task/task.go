package task

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"code.uber.internal/infra/peloton/common"

	log "github.com/sirupsen/logrus"
	"go.uber.org/yarpc/yarpcerrors"
)

const (
	// _defaultMaxParallelBatches indicates how many maximum parallel go routines will
	// be run to perform action on tasks of a job
	_defaultMaxParallelBatches = 1000
)

type singleTask func(id uint32) error

// RunInParallel runs go routines which will perform action on
// given list of instances
func RunInParallel(identifier string, idList []uint32, task singleTask) error {
	var transientError int32

	nTasks := uint32(len(idList))
	// indicates if the task operation hit a transient error
	transientError = 0

	// how many task operations failed due to errors
	tasksNotRun := uint32(0)

	// Each go routine will update at least (nTasks / _defaultMaxParallelBatches)
	// number of tasks. In addition if nTasks % _defaultMaxParallelBatches > 0,
	// the first increment number of go routines are going to run
	// one additional task.
	increment := nTasks % _defaultMaxParallelBatches

	timeStart := time.Now()
	wg := new(sync.WaitGroup)
	prevEnd := uint32(0)

	// run the parallel batches
	for i := uint32(0); i < _defaultMaxParallelBatches; i++ {
		// start of the batch
		updateStart := prevEnd
		// end of the batch
		updateEnd := updateStart + (nTasks / _defaultMaxParallelBatches)
		if increment > 0 {
			updateEnd++
			increment--
		}

		if updateEnd > nTasks {
			updateEnd = nTasks
		}
		prevEnd = updateEnd
		if updateStart == updateEnd {
			continue
		}
		wg.Add(1)

		go func() {
			defer wg.Done()
			for k := updateStart; k < updateEnd; k++ {
				instance := idList[k]
				err := task(instance)
				if err != nil {
					log.WithError(err).
						WithFields(log.Fields{
							"id":          identifier,
							"instance_id": instance,
						}).Info("failed to add workflow event for instance")
					atomic.AddUint32(&tasksNotRun, 1)
					if common.IsTransientError(err) {
						atomic.StoreInt32(&transientError, 1)
					}
					return
				}
			}
		}()
	}
	// wait for all batches to complete
	wg.Wait()

	if tasksNotRun != 0 {
		msg := fmt.Sprintf(
			"task operation succeeded for %d instances of %v,"+
				" and failed for tasks %d in %v",
			nTasks-tasksNotRun,
			identifier,
			tasksNotRun,
			time.Since(timeStart))
		if transientError > 0 {
			return yarpcerrors.AbortedErrorf(msg)
		}
		return yarpcerrors.InternalErrorf(msg)
	}
	return nil
}