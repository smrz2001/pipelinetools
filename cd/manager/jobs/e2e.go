package jobs

import (
	"fmt"
	"os"
	"time"

	"github.com/3box/pipeline-tools/cd/manager"
)

// Allow up to 2 hours for E2E tests to run
const FailureTime = 2 * time.Hour

var _ manager.Job = &e2eTestJob{}

type e2eTestJob struct {
	state manager.JobState
	db    manager.Database
	d     manager.Deployment
}

func E2eTestJob(db manager.Database, d manager.Deployment, jobState manager.JobState) *e2eTestJob {
	return &e2eTestJob{jobState, db, d}
}

func (e e2eTestJob) AdvanceJob() error {
	if e.state.Stage == manager.JobStage_Queued {
		if err := e.startE2eTests(); err != nil {
			e.state.Stage = manager.JobStage_Failed
		} else {
			e.state.Stage = manager.JobStage_Started
		}
	} else if time.Now().Add(-FailureTime).After(e.state.Ts) {
		e.state.Stage = manager.JobStage_Failed
	} else if e.state.Stage == manager.JobStage_Started {
		// Check if all suites started successfully
		if running, err := e.checkE2eTests(true); err != nil {
			e.state.Stage = manager.JobStage_Failed
		} else if running {
			e.state.Stage = manager.JobStage_Waiting
		} else {
			// Return so we come back again to check
			return nil
		}
	} else if e.state.Stage == manager.JobStage_Waiting {
		// Check if all suites completed
		if stopped, err := e.checkE2eTests(false); err != nil {
			e.state.Stage = manager.JobStage_Failed
		} else if stopped {
			e.state.Stage = manager.JobStage_Completed
		} else {
			// Return so we come back again to check
			return nil
		}
	} else {
		// There's nothing left to do so we shouldn't have reached here
		return fmt.Errorf("anchorJob: unexpected state: %s", manager.PrintJob(e.state))
	}
	e.state.Ts = time.Now()
	return e.db.UpdateJob(e.state)
}

func (e e2eTestJob) startE2eTests() error {
	if err := e.startE2eTest(manager.E2eTest_PrivatePublic); err != nil {
		return err
	} else if err = e.startE2eTest(manager.E2eTest_LocalClientPublic); err != nil {
		return err
	} else {
		return e.startE2eTest(manager.E2eTest_LocalNodePrivate)
	}
}

func (e e2eTestJob) startE2eTest(config string) error {
	if id, err := e.d.LaunchService(
		"ceramic-qa-tests",
		"ceramic-qa-tests-e2e_tests",
		"ceramic-qa-tests-e2e_tests",
		"e2e_tests",
		map[string]string{
			"NODE_ENV":              string(config),
			"ETH_RPC_URL":           os.Getenv("ETH_RPC_URL"),
			"AWS_ACCESS_KEY_ID":     os.Getenv("AWS_ACCESS_KEY_ID"),
			"AWS_SECRET_ACCESS_KEY": os.Getenv("AWS_SECRET_ACCESS_KEY"),
			"AWS_REGION":            os.Getenv("AWS_REGION"),
		}); err != nil {
		return err
	} else {
		e.state.Params[config] = id
		return nil
	}
}

func (e e2eTestJob) checkE2eTests(isRunning bool) (bool, error) {
	if privatePublic, err := e.d.CheckTask(isRunning, "ceramic-qa-tests", e.state.Params[manager.E2eTest_PrivatePublic].(string)); err != nil {
		return false, err
	} else if localClientPublic, err := e.d.CheckTask(isRunning, "ceramic-qa-tests", e.state.Params[manager.E2eTest_LocalClientPublic].(string)); err != nil {
		return false, err
	} else if localNodePrivate, err := e.d.CheckTask(isRunning, "ceramic-qa-tests", e.state.Params[manager.E2eTest_LocalNodePrivate].(string)); err != nil {
		return false, err
	} else if privatePublic && localClientPublic && localNodePrivate {
		return true, nil
	}
	return false, nil
}
