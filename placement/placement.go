package placement

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/golang/protobuf/proto"
	"github.com/uber-go/tally"
	"go.uber.org/yarpc"
	"go.uber.org/yarpc/encoding/json"

	"peloton/api/peloton"
	"peloton/api/task"
	"peloton/private/hostmgr/hostsvc"
	"peloton/private/resmgr"
	"peloton/private/resmgrsvc"

	"code.uber.internal/infra/peloton/hostmgr/scalar"
)

const (
	// GetOfferTimeout is the timeout value for get offer request
	GetOfferTimeout = 1 * time.Second
	// GetTaskTimeout is the timeout value for get task request
	GetTaskTimeout = 1 * time.Second
)

// Engine is an interface implementing a way to Start and Stop the
// placement engine
type Engine interface {
	Start()
	Stop()
}

// New creates a new placement engine
func New(
	d yarpc.Dispatcher,
	parent tally.Scope,
	cfg *Config,
	resMgrClientName string,
	hostMgrClientName string) Engine {

	s := placementEngine{
		cfg:           cfg,
		resMgrClient:  json.New(d.ClientConfig(resMgrClientName)),
		hostMgrClient: json.New(d.ClientConfig(hostMgrClientName)),
		rootCtx:       context.Background(),
		metrics:       NewMetrics(parent.SubScope("placement")),
	}
	return &s
}

type placementEngine struct {
	cfg           *Config
	resMgrClient  json.Client
	hostMgrClient json.Client
	rootCtx       context.Context
	started       int32
	shutdown      int32
	metrics       *Metrics
	tick          <-chan time.Time
}

// Start starts placement engine
func (s *placementEngine) Start() {
	if atomic.CompareAndSwapInt32(&s.started, 0, 1) {
		log.Info("Placement Engine starting")
		s.metrics.Running.Update(1)
		go func() {
			// TODO: We need to revisit here if we want to run multiple
			// Placement engine threads
			for s.isRunning() {
				s.placeRound()
			}
		}()
	}
	log.Info("Placement Engine started")
}

// Stop stops placement engine
func (s *placementEngine) Stop() {
	log.Info("Placement Engine stopping")
	s.metrics.Running.Update(0)
	atomic.StoreInt32(&s.shutdown, 1)
}

// placeTaskGroup is the internal loop that makes placement decisions on a group of tasks
// with same grouping constraint.
func (s *placementEngine) placeTaskGroup(group *taskGroup) {
	log.WithField("group", group).Debug("Placing task group")
	totalTasks := len(group.tasks)
	// TODO: move this loop out to the call site of current function,
	//       so we don't need to loop in the test code.
	placementDeadline := time.Now().Add(s.cfg.MaxPlacementDuration)
	for time.Now().Before(placementDeadline) && s.isRunning() {
		if len(group.tasks) == 0 {
			log.Debug("Finishing place task group loop because all tasks are placed")
			return
		}

		hostOffers, err := s.AcquireHostOffers(group)
		// TODO: Add a stopping condition so this does not loop forever.
		if err != nil {
			log.WithField("error", err).Error("Failed to dequeue offer")
			s.metrics.OfferGetFail.Inc(1)
			time.Sleep(GetOfferTimeout)
			continue
		}

		if len(hostOffers) == 0 {
			s.metrics.OfferStarved.Inc(1)
			log.Warn("Empty hostOffers received")
			time.Sleep(GetOfferTimeout)
			continue
		}
		s.metrics.OfferGet.Inc(1)

		// Creating the placements for all the host offers
		var placements []*resmgr.Placement
		var placement *resmgr.Placement
		index := 0
		for _, hostOffer := range hostOffers {
			if len(group.tasks) <= 0 {
				break
			}
			placement, group.tasks = s.placeTasks(
				group.tasks,
				group.getResourceConfig(),
				hostOffer,
			)
			placements = append(placements, placement)
			index++
		}

		// Setting the placements for all the placements
		err = s.setPlacements(placements)

		if err != nil {
			// If there is error in placement returning all the host offer
			s.returnUnused(hostOffers)
		} else {
			unused := hostOffers[index:]
			if len(unused) > 0 {
				s.returnUnused(unused)
			}
		}

		log.WithField("remaining_tasks", group.tasks).
			Debug("Tasks remaining for next placeTaskGroup")
	}
	if len(group.tasks) > 0 {
		log.WithFields(log.Fields{
			"Tasks Remaining": len(group.tasks),
			"Tasks Total":     totalTasks,
		}).Warn("Could not place Tasks due to insufficiant Offers")

		log.WithField("task_group", group.tasks).
			Debug("Task group still has remaining tasks " +
				"after allowed duration")
		// TODO: add metrics for this
		// TODO: send unplaced tasks back to correct state (READY).
	}
}

// returnUnused returns unused host offers back to host manager.
func (s *placementEngine) returnUnused(hostOffers []*hostsvc.HostOffer) error {
	ctx, cancelFunc := context.WithTimeout(s.rootCtx, 10*time.Second)
	defer cancelFunc()
	var response hostsvc.ReleaseHostOffersResponse
	var request = &hostsvc.ReleaseHostOffersRequest{
		HostOffers: hostOffers,
	}
	_, err := s.hostMgrClient.Call(
		ctx,
		yarpc.NewReqMeta().Procedure("InternalHostService.ReleaseHostOffers"),
		request,
		&response,
	)

	if err != nil {
		log.WithField("error", err).Error("ReleaseHostOffers failed")
		return err
	}

	if respErr := response.GetError(); respErr != nil {
		log.WithField("error", respErr).Error("ReleaseHostOffers error")
		// TODO: Differentiate known error types by metrics and logs.
		return errors.New(respErr.String())
	}

	log.WithField("host_offers", hostOffers).Debug("Returned unused host offers")
	return nil
}

// AcquireHostOffers calls hostmgr and obtain HostOffers for given task group.
func (s *placementEngine) AcquireHostOffers(group *taskGroup) ([]*hostsvc.HostOffer, error) {
	// Right now, this limits number of hosts to request from hostsvc.
	// In the longer term, we should consider converting this to total resources necessary.
	limit := s.cfg.OfferDequeueLimit
	if len(group.tasks) < limit {
		limit = len(group.tasks)
	}

	// Make a deep copy because we are modifying this struct here.
	constraint := proto.Clone(&group.constraint).(*hostsvc.Constraint)
	constraint.HostLimit = uint32(limit)
	ctx, cancelFunc := context.WithTimeout(s.rootCtx, 10*time.Second)
	defer cancelFunc()
	var response hostsvc.AcquireHostOffersResponse
	var request = &hostsvc.AcquireHostOffersRequest{
		Constraint: constraint,
	}

	log.WithField("request", request).Debug("Calling AcquireHostOffers")

	_, err := s.hostMgrClient.Call(
		ctx,
		yarpc.NewReqMeta().Procedure("InternalHostService.AcquireHostOffers"),
		request,
		&response,
	)

	if err != nil {
		log.WithField("error", err).Error("AcquireHostOffers failed")
		return nil, err
	}

	log.WithField("response", response).Debug("AcquireHostOffers returned")

	if respErr := response.GetError(); respErr != nil {
		log.WithField("error", respErr).Error("AcquireHostOffers error")
		// TODO: Differentiate known error types by metrics and logs.
		return nil, errors.New(respErr.String())
	}

	result := response.GetHostOffers()
	return result, nil
}

// placeTasks takes the tasks and convert them to placements
func (s *placementEngine) placeTasks(
	tasks []*resmgr.Task,
	resourceConfig *task.ResourceConfig,
	hostOffer *hostsvc.HostOffer) (*resmgr.Placement, []*resmgr.Task) {

	if len(tasks) == 0 {
		log.Debug("No task to place")
		return nil, tasks
	}

	usage := scalar.FromResourceConfig(resourceConfig)
	remain := scalar.FromMesosResources(hostOffer.GetResources())

	var selectedTasks []*resmgr.Task
	for i := 0; i < len(tasks); i++ {
		trySubtract := remain.TrySubtract(&usage)
		if trySubtract == nil {
			// NOTE: current placement implementation means all
			// tasks in the same group has the same resource configuration.
			log.WithFields(log.Fields{
				"remain": remain,
				"usage":  usage,
			}).Debug("Insufficient resource in remain")
			break
		}
		remain = *trySubtract
		selectedTasks = append(selectedTasks, tasks[i])
	}
	tasks = tasks[len(selectedTasks):]

	log.WithFields(log.Fields{
		"selected_tasks":  selectedTasks,
		"remaining_tasks": tasks,
	}).Debug("Selected tasks to place")

	if len(selectedTasks) <= 0 {
		return nil, tasks
	}
	placement := s.createTasksPlacement(selectedTasks, hostOffer)

	return placement, tasks
}

func (s *placementEngine) setPlacements(placements []*resmgr.Placement) error {
	if len(placements) == 0 {
		log.Debug("No task to place")
		err := errors.New("No placements to set")
		return err
	}
	watcher := s.metrics.SetPlacementDuration.Start()
	ctx, cancelFunc := context.WithTimeout(s.rootCtx, 10*time.Second)
	defer cancelFunc()
	var response resmgrsvc.SetPlacementsResponse
	var request = &resmgrsvc.SetPlacementsRequest{
		Placements: placements,
	}
	log.WithField("request", request).Debug("Calling SetPlacements")
	_, err := s.resMgrClient.Call(
		ctx,
		yarpc.NewReqMeta().Procedure("ResourceManagerService.SetPlacements"),
		request,
		&response,
	)
	// TODO: add retry / put back offer and tasks in failure scenarios
	if err != nil {
		log.WithFields(log.Fields{
			"num_placements": len(placements),
			"error":          err.Error(),
		}).WithError(errors.New("Failed to set placements"))

		s.metrics.SetPlacementFail.Inc(1)
		return err
	}

	log.WithField("response", response).Debug("Place Tasks returned")

	if response.Error != nil {
		log.WithFields(log.Fields{
			"num_placements": len(placements),
			"error":          response.Error.String(),
		}).Error("Failed to place tasks")
		s.metrics.SetPlacementFail.Inc(1)
		return errors.New("Failed to place tasks")
	}
	lenTasks := 0
	for _, p := range placements {
		lenTasks += len(p.Tasks)
	}
	s.metrics.SetPlacementSuccess.Inc(int64(len(placements)))
	s.metrics.SetPlacementDuration.Record(watcher.Stop())

	log.WithFields(log.Fields{
		"num_placements": len(placements),
	}).Info("Set placements")
	return nil
}

// createTasksPlacement creates the placement for resource manager
// It also returns the list of tasks which can not be placed
func (s *placementEngine) createTasksPlacement(tasks []*resmgr.Task,
	hostOffer *hostsvc.HostOffer) *resmgr.Placement {
	watcher := s.metrics.CreatePlacementDuration.Start()
	var tasksIds []*peloton.TaskID
	for _, t := range tasks {
		taskID := t.Id
		tasksIds = append(tasksIds, taskID)
	}
	placement := &resmgr.Placement{
		AgentId:  hostOffer.AgentId,
		Hostname: hostOffer.Hostname,
		Tasks:    tasksIds,
		// TODO : We are not setting offerId's
		// we need to remove it from protobuf
	}

	log.WithFields(log.Fields{
		"num_tasks": len(tasksIds),
	}).Info("Create Placements")
	s.metrics.CreatePlacementDuration.Record(watcher.Stop())
	return placement
}

func (s *placementEngine) isRunning() bool {
	shutdown := atomic.LoadInt32(&s.shutdown)
	return shutdown == 0
}

// placeRound tries one round of placement action
func (s *placementEngine) placeRound() {
	tasks, err := s.getTasks(s.cfg.TaskDequeueLimit)
	if err != nil {
		log.WithField("error", err).Error("Failed to dequeue tasks")
		time.Sleep(GetTaskTimeout)
		return
	}
	if len(tasks) == 0 {
		log.Debug("No task to place in workLoop")
		time.Sleep(GetTaskTimeout)
		return
	}
	log.WithField("tasks", len(tasks)).Info("Dequeued from task queue")
	taskGroups := groupTasks(tasks)
	for _, tg := range taskGroups {
		// Launching go routine per task group
		// TODO: We need to change this worker thread pool model
		go func(group *taskGroup) {
			s.placeTaskGroup(group)
		}(tg)
	}
}

// getTasks deques tasks from task queue in resource manager
func (s *placementEngine) getTasks(limit int) (
	taskInfos []*resmgr.Task, err error) {
	// It could happen that the work loop is started before the
	// peloton master inbound is started.  In such case it could
	// panic. This we capture the panic, return error, wait then
	// resume
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Recovered from panic %v", r)
		}
	}()

	ctx, cancelFunc := context.WithTimeout(s.rootCtx, 10*time.Second)
	defer cancelFunc()
	var response resmgrsvc.DequeueTasksResponse
	var request = &resmgrsvc.DequeueTasksRequest{
		Limit:   uint32(limit),
		Timeout: uint32(s.cfg.TaskDequeueTimeOut),
	}

	log.WithField("request", request).Debug("Dequeuing tasks")

	_, err = s.resMgrClient.Call(
		ctx,
		yarpc.NewReqMeta().Procedure("ResourceManagerService.DequeueTasks"),
		request,
		&response,
	)
	if err != nil {
		log.WithField("error", err).Error("Dequeue failed")
		return nil, err
	}

	log.WithField("tasks", response.Tasks).Debug("Dequeued tasks")

	return response.Tasks, nil
}

type taskGroup struct {
	constraint hostsvc.Constraint
	tasks      []*resmgr.Task
}

func (g *taskGroup) getResourceConfig() *task.ResourceConfig {
	return g.constraint.GetResourceConstraint().GetMinimum()
}

func getHostSvcConstraint(t *resmgr.Task) hostsvc.Constraint {
	result := hostsvc.Constraint{
		// HostLimit will be later determined by number of tasks.
		ResourceConstraint: &hostsvc.ResourceConstraint{
			Minimum: t.Resource,
		},
	}
	if t.Constraint != nil {
		result.SchedulingConstraint = t.Constraint
	}
	return result
}

// groupTasks groups tasks based on call constraint to hostsvc.
// Returns grouped tasks keyed by serialized hostsvc.Constraint.
func groupTasks(tasks []*resmgr.Task) map[string]*taskGroup {
	groups := make(map[string]*taskGroup)
	for _, t := range tasks {
		c := getHostSvcConstraint(t)
		// String() function on protobuf message should be nil-safe.
		s := c.String()
		if _, ok := groups[s]; !ok {
			groups[s] = &taskGroup{
				constraint: c,
				tasks:      []*resmgr.Task{},
			}
		}
		groups[s].tasks = append(groups[s].tasks, t)
	}
	return groups
}
