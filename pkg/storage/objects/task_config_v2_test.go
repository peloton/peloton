// Copyright (c) 2019 Uber Technologies, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package objects

import (
	"context"
	"testing"

	"github.com/uber/peloton/.gen/peloton/api/v0/peloton"
	pbtask "github.com/uber/peloton/.gen/peloton/api/v0/task"
	v1alphapeloton "github.com/uber/peloton/.gen/peloton/api/v1alpha/peloton"
	pbpod "github.com/uber/peloton/.gen/peloton/api/v1alpha/pod"
	"github.com/uber/peloton/.gen/peloton/private/models"

	"github.com/pborman/uuid"
	"github.com/stretchr/testify/suite"
)

type TaskConfigV2ObjectTestSuite struct {
	suite.Suite
	jobID *peloton.JobID
}

func (s *TaskConfigV2ObjectTestSuite) SetupTest() {
	setupTestStore()
	s.jobID = &peloton.JobID{Value: uuid.New()}
}

func TestTaskConfigV2ObjectTestSuite(t *testing.T) {
	suite.Run(t, new(TaskConfigV2ObjectTestSuite))
}

func (s *TaskConfigV2ObjectTestSuite) TestCreateGetPodSpec() {
	var configVersion uint64 = 1
	var instance0 int64 = 0
	var instance1 int64 = 1

	db := NewTaskConfigV2OpsOps(testStore)
	ctx := context.Background()

	taskConfig := &pbtask.TaskConfig{
		Name: "test-task",
		Resource: &pbtask.ResourceConfig{
			CpuLimit:    0.8,
			MemLimitMb:  800,
			DiskLimitMb: 1500,
		},
	}

	podSpec := &pbpod.PodSpec{
		PodName:    &v1alphapeloton.PodName{Value: "test-pod"},
		Containers: []*pbpod.ContainerSpec{{}},
	}

	s.NoError(db.Create(
		ctx,
		s.jobID,
		instance0,
		taskConfig,
		&models.ConfigAddOn{},
		podSpec,
		configVersion,
	))

	// test normal get
	spec, err := db.GetPodSpec(
		ctx,
		s.jobID,
		uint32(instance0),
		configVersion,
	)
	s.NoError(err)
	s.Equal(podSpec, spec)

	// test get from a non-existent job
	spec, err = db.GetPodSpec(
		ctx,
		&peloton.JobID{Value: uuid.New()},
		uint32(instance0),
		configVersion,
	)
	s.Error(err)
	s.Nil(spec)

	// test create and read a nil pod spec
	s.NoError(db.Create(
		ctx,
		s.jobID,
		instance1,
		taskConfig,
		&models.ConfigAddOn{},
		nil,
		configVersion,
	))

	spec, err = db.GetPodSpec(
		ctx,
		s.jobID,
		uint32(instance1),
		configVersion,
	)
	s.NoError(err)
	s.Nil(spec)
}