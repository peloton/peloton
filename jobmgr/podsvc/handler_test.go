package podsvc

import (
	"context"
	"fmt"
	"testing"
	"time"

	cachedmocks "code.uber.internal/infra/peloton/jobmgr/cached/mocks"
	goalstatemocks "code.uber.internal/infra/peloton/jobmgr/goalstate/mocks"
	leadermocks "code.uber.internal/infra/peloton/leader/mocks"
	storemocks "code.uber.internal/infra/peloton/storage/mocks"

	pbjob "code.uber.internal/infra/peloton/.gen/peloton/api/v0/job"
	"code.uber.internal/infra/peloton/.gen/peloton/api/v0/peloton"
	pbtask "code.uber.internal/infra/peloton/.gen/peloton/api/v0/task"
	v1alphapeloton "code.uber.internal/infra/peloton/.gen/peloton/api/v1alpha/peloton"
	"code.uber.internal/infra/peloton/.gen/peloton/api/v1alpha/pod"
	"code.uber.internal/infra/peloton/.gen/peloton/api/v1alpha/pod/svc"

	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/suite"
	"go.uber.org/yarpc/yarpcerrors"
)

const (
	testJobID      = "941ff353-ba82-49fe-8f80-fb5bc649b04d"
	testInstanceID = 1
	testPodName    = "941ff353-ba82-49fe-8f80-fb5bc649b04d-1"
	testPodID      = "941ff353-ba82-49fe-8f80-fb5bc649b04d-1-3"
)

type podHandlerTestSuite struct {
	suite.Suite

	handler *serviceHandler

	ctrl            *gomock.Controller
	cachedJob       *cachedmocks.MockJob
	cachedTask      *cachedmocks.MockTask
	jobFactory      *cachedmocks.MockJobFactory
	candidate       *leadermocks.MockCandidate
	podStore        *storemocks.MockTaskStore
	goalStateDriver *goalstatemocks.MockDriver
}

func (suite *podHandlerTestSuite) SetupTest() {
	suite.ctrl = gomock.NewController(suite.T())
	suite.cachedJob = cachedmocks.NewMockJob(suite.ctrl)
	suite.cachedTask = cachedmocks.NewMockTask(suite.ctrl)
	suite.jobFactory = cachedmocks.NewMockJobFactory(suite.ctrl)
	suite.podStore = storemocks.NewMockTaskStore(suite.ctrl)
	suite.candidate = leadermocks.NewMockCandidate(suite.ctrl)
	suite.goalStateDriver = goalstatemocks.NewMockDriver(suite.ctrl)
	suite.handler = &serviceHandler{
		jobFactory:      suite.jobFactory,
		candidate:       suite.candidate,
		podStore:        suite.podStore,
		goalStateDriver: suite.goalStateDriver,
	}
}

func (suite *podHandlerTestSuite) TearDownTest() {
	suite.ctrl.Finish()
}

// GetPodCacheSuccess test the success case of get pod cache
func (suite *podHandlerTestSuite) TestGetPodCacheSuccess() {
	suite.jobFactory.EXPECT().
		GetJob(&peloton.JobID{Value: testJobID}).
		Return(suite.cachedJob)

	suite.cachedJob.EXPECT().
		GetTask(uint32(testInstanceID)).
		Return(suite.cachedTask)

	suite.cachedTask.EXPECT().
		GetRunTime(gomock.Any()).
		Return(&pbtask.RuntimeInfo{
			State:     pbtask.TaskState_RUNNING,
			GoalState: pbtask.TaskState_KILLED,
			Healthy:   pbtask.HealthState_HEALTHY,
		}, nil)

	resp, err := suite.handler.GetPodCache(context.Background(),
		&svc.GetPodCacheRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})
	suite.NoError(err)
	suite.NotNil(resp.GetStatus())
	suite.Equal(resp.GetStatus().GetState(), pod.PodState_POD_STATE_RUNNING)
	suite.Equal(resp.GetStatus().GetDesiredState(), pod.PodState_POD_STATE_KILLED)
	suite.Equal(resp.GetStatus().GetHealthy(), pod.HealthState_HEALTH_STATE_HEALTHY)
}

// TestGetPodCacheInvalidPodName test the case of getting cache
// with invalid pod name
func (suite *podHandlerTestSuite) TestGetPodCacheInvalidPodName() {
	resp, err := suite.handler.GetPodCache(context.Background(),
		&svc.GetPodCacheRequest{
			PodName: &v1alphapeloton.PodName{Value: "invalid-name"},
		})
	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsInvalidArgument(err))
}

// TestGetPodCacheNoJobCache tests the case of getting cache
// when the corresponding job cache does not exist
func (suite *podHandlerTestSuite) TestGetPodCacheNoJobCache() {
	suite.jobFactory.EXPECT().
		GetJob(&peloton.JobID{Value: testJobID}).
		Return(suite.cachedJob)

	suite.cachedJob.EXPECT().
		GetTask(uint32(testInstanceID)).
		Return(nil)

	resp, err := suite.handler.GetPodCache(context.Background(),
		&svc.GetPodCacheRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})
	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsNotFound(err))
}

// TestGetPodCacheNoTaskCache tests the case of getting cache
// when cachedTask fail to get runtime
func (suite *podHandlerTestSuite) TestGetPodCacheNoTaskCache() {
	suite.jobFactory.EXPECT().
		GetJob(&peloton.JobID{Value: testJobID}).
		Return(suite.cachedJob)

	suite.cachedJob.EXPECT().
		GetTask(uint32(testInstanceID)).
		Return(nil)

	resp, err := suite.handler.GetPodCache(context.Background(),
		&svc.GetPodCacheRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})
	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsNotFound(err))
}

// TestGetPodCacheFailToGetRuntime tests the case of getting cache
// when the corresponding task cache does not exist
func (suite *podHandlerTestSuite) TestGetPodCacheFailToGetRuntime() {
	suite.jobFactory.EXPECT().
		GetJob(&peloton.JobID{Value: testJobID}).
		Return(suite.cachedJob)

	suite.cachedJob.EXPECT().
		GetTask(uint32(testInstanceID)).
		Return(suite.cachedTask)

	suite.cachedTask.EXPECT().
		GetRunTime(gomock.Any()).
		Return(nil, yarpcerrors.UnavailableErrorf("test error"))

	resp, err := suite.handler.GetPodCache(context.Background(),
		&svc.GetPodCacheRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})
	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsUnavailable(err))
}

// TestRefreshPodSuccess tests the success case of refreshing pod
func (suite *podHandlerTestSuite) TestRefreshPodSuccess() {
	taskRuntime := &pbtask.RuntimeInfo{
		State: pbtask.TaskState_RUNNING,
	}
	pelotonJobID := &peloton.JobID{Value: testJobID}

	gomock.InOrder(
		suite.candidate.EXPECT().
			IsLeader().
			Return(true),

		suite.podStore.EXPECT().
			GetTaskRuntime(gomock.Any(), pelotonJobID, uint32(testInstanceID)).
			Return(taskRuntime, nil),

		suite.jobFactory.EXPECT().
			AddJob(pelotonJobID).
			Return(suite.cachedJob),

		suite.cachedJob.EXPECT().
			ReplaceTasks(map[uint32]*pbtask.RuntimeInfo{
				testInstanceID: taskRuntime,
			}, true).
			Return(nil),

		suite.goalStateDriver.EXPECT().
			EnqueueTask(pelotonJobID, uint32(testInstanceID), gomock.Any()).
			Return(),

		suite.cachedJob.EXPECT().
			GetJobType().
			Return(pbjob.JobType_SERVICE),

		suite.goalStateDriver.EXPECT().
			JobRuntimeDuration(pbjob.JobType_SERVICE).
			Return(time.Second),

		suite.goalStateDriver.EXPECT().
			EnqueueJob(pelotonJobID, gomock.Any()).
			Return(),
	)

	resp, err := suite.handler.RefreshPod(context.Background(),
		&svc.RefreshPodRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})
	suite.NotNil(resp)
	suite.NoError(err)
}

// TestRefreshPodNonLeader tests calling refresh pod
// on non-leader jobmgr
func (suite *podHandlerTestSuite) TestRefreshPodNonLeader() {
	suite.candidate.EXPECT().
		IsLeader().
		Return(false)

	resp, err := suite.handler.RefreshPod(context.Background(),
		&svc.RefreshPodRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})
	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsUnavailable(err))
}

// TestRefreshPodInvalidPodName tests the failure case of refreshing pod
// due to invalid pod name
func (suite *podHandlerTestSuite) TestRefreshPodInvalidPodName() {
	suite.candidate.EXPECT().
		IsLeader().
		Return(true)

	resp, err := suite.handler.RefreshPod(context.Background(),
		&svc.RefreshPodRequest{
			PodName: &v1alphapeloton.PodName{Value: "invalid-name"},
		})
	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsInvalidArgument(err))
}

// TestRefreshPodFailToGetTaskRuntime tests the failure
// case of refreshing pod, due to error while getting task runtime
func (suite *podHandlerTestSuite) TestRefreshPodFailToGetTaskRuntime() {
	pelotonJobID := &peloton.JobID{Value: testJobID}

	suite.candidate.EXPECT().
		IsLeader().
		Return(true)

	suite.podStore.EXPECT().
		GetTaskRuntime(gomock.Any(), pelotonJobID, uint32(testInstanceID)).
		Return(nil, yarpcerrors.InternalErrorf("test error"))

	resp, err := suite.handler.RefreshPod(context.Background(),
		&svc.RefreshPodRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})

	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsInternal(err))
}

// TestRefreshPodFailToReplaceTasks tests the failure case of
// replacing tasks
func (suite *podHandlerTestSuite) TestRefreshPodFailToReplaceTasks() {
	taskRuntime := &pbtask.RuntimeInfo{
		State: pbtask.TaskState_RUNNING,
	}
	pelotonJobID := &peloton.JobID{Value: testJobID}

	gomock.InOrder(
		suite.candidate.EXPECT().
			IsLeader().
			Return(true),

		suite.podStore.EXPECT().
			GetTaskRuntime(gomock.Any(), pelotonJobID, uint32(testInstanceID)).
			Return(taskRuntime, nil),

		suite.jobFactory.EXPECT().
			AddJob(pelotonJobID).
			Return(suite.cachedJob),

		suite.cachedJob.EXPECT().
			ReplaceTasks(map[uint32]*pbtask.RuntimeInfo{
				testInstanceID: taskRuntime,
			}, true).
			Return(yarpcerrors.InternalErrorf("test error")),
	)

	resp, err := suite.handler.RefreshPod(context.Background(),
		&svc.RefreshPodRequest{
			PodName: &v1alphapeloton.PodName{Value: testPodName},
		})
	suite.Nil(resp)
	suite.Error(err)
	suite.True(yarpcerrors.IsInternal(err))
}

func TestPodServiceHandler(t *testing.T) {
	suite.Run(t, new(podHandlerTestSuite))
}

func (suite *podHandlerTestSuite) TestStartPod() {
	request := &svc.StartPodRequest{}
	response, err := suite.handler.StartPod(context.Background(), request)
	suite.NoError(err)
	suite.NotNil(response)
}

func (suite *podHandlerTestSuite) TestStopPod() {
	request := &svc.StopPodRequest{}
	response, err := suite.handler.StopPod(context.Background(), request)
	suite.NoError(err)
	suite.NotNil(response)
}

func (suite *podHandlerTestSuite) TestRestartPod() {
	request := &svc.RestartPodRequest{}
	response, err := suite.handler.RestartPod(context.Background(), request)
	suite.NoError(err)
	suite.NotNil(response)
}

func (suite *podHandlerTestSuite) TestGetPod() {
	request := &svc.GetPodRequest{}
	response, err := suite.handler.GetPod(context.Background(), request)
	suite.NoError(err)
	suite.NotNil(response)
}

// TestServiceHandler_GetPodEvents tests getting pod events for a given pod
func (suite *podHandlerTestSuite) TestGetPodEvents() {
	request := &svc.GetPodEventsRequest{
		PodName: &v1alphapeloton.PodName{
			Value: testPodName,
		},
	}

	events := []*pod.PodEvent{
		{
			PodId: &v1alphapeloton.PodID{
				Value: testPodID,
			},
			ActualState:  "STARTING",
			DesiredState: "RUNNING",
			PrevPodId: &v1alphapeloton.PodID{
				Value: "0",
			},
		},
	}
	response := &svc.GetPodEventsResponse{
		Events: events,
	}

	suite.podStore.EXPECT().
		GetPodEvents(gomock.Any(), testJobID, uint32(testInstanceID), "").
		Return(events, nil)
	response, err := suite.handler.GetPodEvents(context.Background(), request)
	suite.NoError(err)
	suite.Equal(events, response.GetEvents())
}

// TestGetPodEventsPodNameParseError tests PodName parse error
// while getting pod events for a given pod
func (suite *podHandlerTestSuite) TestGetPodEventsPodNameParseError() {
	request := &svc.GetPodEventsRequest{}
	_, err := suite.handler.GetPodEvents(context.Background(), request)
	suite.Error(err)
}

// TestGetPodEventsStoreError tests store error
// while getting pod events for a given pod
func (suite *podHandlerTestSuite) TestGetPodEventsStoreError() {
	request := &svc.GetPodEventsRequest{
		PodName: &v1alphapeloton.PodName{
			Value: testPodName,
		},
	}
	suite.podStore.EXPECT().
		GetPodEvents(gomock.Any(), testJobID, uint32(testInstanceID), "").
		Return(nil, fmt.Errorf("fake GetPodEvents error"))
	_, err := suite.handler.GetPodEvents(context.Background(), request)
	suite.Error(err)
}

func (suite *podHandlerTestSuite) TestBrowsePodSandbox() {
	request := &svc.BrowsePodSandboxRequest{}
	response, err := suite.handler.BrowsePodSandbox(context.Background(), request)
	suite.NoError(err)
	suite.NotNil(response)
}

func (suite *podHandlerTestSuite) TestDeletePodEvents() {
	request := &svc.DeletePodEventsRequest{}
	response, err := suite.handler.DeletePodEvents(context.Background(), request)
	suite.NoError(err)
	suite.NotNil(response)
}