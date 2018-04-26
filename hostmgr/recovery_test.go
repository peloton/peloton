package hostmgr

import (
	"fmt"
	"testing"

	"code.uber.internal/infra/peloton/.gen/mesos/v1"
	"code.uber.internal/infra/peloton/.gen/mesos/v1/maintenance"
	"code.uber.internal/infra/peloton/.gen/mesos/v1/master"

	"code.uber.internal/infra/peloton/hostmgr/queue/mocks"
	mpb_mocks "code.uber.internal/infra/peloton/yarpc/encoding/mpb/mocks"

	"github.com/golang/mock/gomock"
	log "github.com/sirupsen/logrus"
	"github.com/stretchr/testify/suite"
	"github.com/uber-go/tally"
)

type RecoveryTestSuite struct {
	suite.Suite
	mockCtrl                 *gomock.Controller
	handler                  RecoveryHandler
	mockMaintenanceQueue     *mocks.MockMaintenanceQueue
	mockMasterOperatorClient *mpb_mocks.MockMasterOperatorClient
	drainingMachines         []*mesos_v1.MachineID
	downMachines             []*mesos_v1.MachineID
}

func (suite *RecoveryTestSuite) SetupSuite() {
	drainingHostname := "draininghost"
	drainingIP := "172.17.0.6"
	drainingMachine := &mesos_v1.MachineID{
		Hostname: &drainingHostname,
		Ip:       &drainingIP,
	}
	suite.drainingMachines = append(suite.drainingMachines, drainingMachine)

	downHostname := "downhost"
	downIP := "172.17.0.7"
	downMachine := &mesos_v1.MachineID{
		Hostname: &downHostname,
		Ip:       &downIP,
	}

	suite.downMachines = append(suite.downMachines, downMachine)
}

func (suite *RecoveryTestSuite) SetupTest() {
	log.Info("setting up test")
	suite.mockCtrl = gomock.NewController(suite.T())
	suite.mockMaintenanceQueue = mocks.NewMockMaintenanceQueue(suite.mockCtrl)
	suite.mockMasterOperatorClient = mpb_mocks.NewMockMasterOperatorClient(suite.mockCtrl)

	suite.handler = NewRecoveryHandler(tally.NoopScope, suite.mockMasterOperatorClient, suite.mockMaintenanceQueue)
}

func (suite *RecoveryTestSuite) TearDownTest() {
	log.Info("tearing down test")
	suite.mockCtrl.Finish()
}

func TestHostmgrRecovery(t *testing.T) {
	suite.Run(t, new(RecoveryTestSuite))
}

func (suite *RecoveryTestSuite) TestRestoreMaintenanceQueue() {
	var clusterDrainingMachines []*mesos_v1_maintenance.ClusterStatus_DrainingMachine
	for _, drainingMachine := range suite.drainingMachines {
		clusterDrainingMachines = append(clusterDrainingMachines, &mesos_v1_maintenance.ClusterStatus_DrainingMachine{
			Id: drainingMachine,
		})
	}

	clusterStatus := &mesos_v1_maintenance.ClusterStatus{
		DrainingMachines: clusterDrainingMachines,
		DownMachines:     suite.downMachines,
	}

	suite.mockMasterOperatorClient.EXPECT().GetMaintenanceStatus().Return(&mesos_v1_master.Response_GetMaintenanceStatus{
		Status: clusterStatus,
	}, nil)

	var drainingHostnames []string
	for _, machine := range suite.drainingMachines {
		drainingHostnames = append(drainingHostnames, machine.GetHostname())
	}
	suite.mockMaintenanceQueue.EXPECT().Enqueue(gomock.Any()).Return(nil).Do(func(hostnames []string) {
		suite.EqualValues(drainingHostnames, hostnames)
	})
	err := suite.handler.Start()
	suite.NoError(err)
}

// Test error while getting maintenance status
func (suite *RecoveryTestSuite) TestGetMaintenanceStatusError() {
	suite.mockMasterOperatorClient.EXPECT().GetMaintenanceStatus().Return(nil, fmt.Errorf("fake GetMaintenanceStatus error"))
	err := suite.handler.Start()
	suite.Error(err)
}

// Test empty maintenance status
func (suite *RecoveryTestSuite) TestEmptyMaintenanceStatus() {
	suite.mockMasterOperatorClient.EXPECT().GetMaintenanceStatus().Return(&mesos_v1_master.Response_GetMaintenanceStatus{}, nil)
	err := suite.handler.Start()
	suite.NoError(err)
}

// Test error while enqueuing into maintenance queue
func (suite *RecoveryTestSuite) TestEnqueueError() {
	var clusterDrainingMachines []*mesos_v1_maintenance.ClusterStatus_DrainingMachine
	for _, drainingMachine := range suite.drainingMachines {
		clusterDrainingMachines = append(clusterDrainingMachines, &mesos_v1_maintenance.ClusterStatus_DrainingMachine{
			Id: drainingMachine,
		})
	}

	clusterStatus := &mesos_v1_maintenance.ClusterStatus{
		DrainingMachines: clusterDrainingMachines,
		DownMachines:     suite.downMachines,
	}
	suite.mockMasterOperatorClient.EXPECT().GetMaintenanceStatus().Return(&mesos_v1_master.Response_GetMaintenanceStatus{
		Status: clusterStatus,
	}, nil)
	suite.mockMaintenanceQueue.EXPECT().Enqueue(gomock.Any()).Return(fmt.Errorf("fake Enqueue error"))
	err := suite.handler.Start()
	suite.Error(err)
}

func (suite *RecoveryTestSuite) TestStop() {
	err := suite.handler.Stop()
	suite.NoError(err)
}
