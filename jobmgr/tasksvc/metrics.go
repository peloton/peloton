package tasksvc

import (
	"github.com/uber-go/tally"
)

// Metrics is the struct containing all the counters that track
// internal state of the job service
type Metrics struct {
	TaskAPIGet        tally.Counter
	TaskGet           tally.Counter
	TaskGetFail       tally.Counter
	TaskAPIGetEvents  tally.Counter
	TaskGetEvents     tally.Counter
	TaskGetEventsFail tally.Counter
	TaskCreate        tally.Counter
	TaskCreateFail    tally.Counter
	TaskAPIList       tally.Counter
	TaskList          tally.Counter
	TaskListFail      tally.Counter
	TaskAPIRefresh    tally.Counter
	TaskRefresh       tally.Counter
	TaskRefreshFail   tally.Counter
	TaskAPIStart      tally.Counter
	TaskStart         tally.Counter
	TaskStartFail     tally.Counter
	TaskAPIStop       tally.Counter
	TaskStop          tally.Counter
	TaskStopFail      tally.Counter
	TaskAPIRestart    tally.Counter
	TaskRestart       tally.Counter
	TaskRestartFail   tally.Counter
	TaskAPIQuery      tally.Counter
	TaskQuery         tally.Counter
	TaskQueryFail     tally.Counter

	TaskAPIListLogs  tally.Counter
	TaskListLogs     tally.Counter
	TaskListLogsFail tally.Counter
}

// NewMetrics returns a new Metrics struct, with all metrics
// initialized and rooted at the given tally.Scope
func NewMetrics(scope tally.Scope) *Metrics {
	taskSuccessScope := scope.Tagged(map[string]string{"result": "success"})
	taskFailScope := scope.Tagged(map[string]string{"result": "fail"})
	taskAPIScope := scope.SubScope("api")

	return &Metrics{
		TaskAPIGet:        taskAPIScope.Counter("get"),
		TaskGet:           taskSuccessScope.Counter("get"),
		TaskGetFail:       taskFailScope.Counter("get"),
		TaskAPIGetEvents:  taskAPIScope.Counter("get_events"),
		TaskGetEvents:     taskSuccessScope.Counter("get_events"),
		TaskGetEventsFail: taskFailScope.Counter("get_events"),
		TaskCreate:        taskSuccessScope.Counter("create"),
		TaskCreateFail:    taskFailScope.Counter("create"),
		TaskAPIList:       taskAPIScope.Counter("list"),
		TaskList:          taskSuccessScope.Counter("list"),
		TaskListFail:      taskFailScope.Counter("list"),
		TaskAPIRefresh:    taskAPIScope.Counter("refresh"),
		TaskRefresh:       taskSuccessScope.Counter("refresh"),
		TaskRefreshFail:   taskFailScope.Counter("refresh"),
		TaskAPIStart:      taskAPIScope.Counter("start"),
		TaskStart:         taskSuccessScope.Counter("start"),
		TaskStartFail:     taskFailScope.Counter("start"),
		TaskAPIStop:       taskAPIScope.Counter("stop"),
		TaskStop:          taskSuccessScope.Counter("stop"),
		TaskStopFail:      taskFailScope.Counter("stop"),
		TaskAPIRestart:    taskAPIScope.Counter("restart"),
		TaskRestart:       taskSuccessScope.Counter("restart"),
		TaskRestartFail:   taskFailScope.Counter("restart"),
		TaskAPIQuery:      taskAPIScope.Counter("query"),
		TaskQuery:         taskSuccessScope.Counter("query"),
		TaskQueryFail:     taskFailScope.Counter("query"),
		TaskAPIListLogs:   taskAPIScope.Counter("list_logs"),
		TaskListLogs:      taskSuccessScope.Counter("list_logs"),
		TaskListLogsFail:  taskFailScope.Counter("list_logs"),
	}
}
