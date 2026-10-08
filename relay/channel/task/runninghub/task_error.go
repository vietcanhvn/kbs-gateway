package runninghub

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/pkg/mediaworkflow"
	rh "github.com/QuantumNous/new-api/pkg/mediaworkflow/runninghub"
)

// taskBuildError carries the TaskError code/status through the generic
// "build_request_failed" wrapper in RelayTaskSubmit (see service.TaskErrorWrapper).
type taskBuildError struct {
	code       string
	message    string
	statusCode int
	local      bool
}

func (e *taskBuildError) Error() string            { return e.message }
func (e *taskBuildError) TaskErrorCode() string    { return e.code }
func (e *taskBuildError) TaskErrorStatusCode() int { return e.statusCode }
func (e *taskBuildError) TaskErrorLocal() bool     { return e.local }
func (e *taskBuildError) TaskErrorData() any       { return nil }

func localRequestError(err error) error {
	return &taskBuildError{code: "invalid_request", message: err.Error(), statusCode: http.StatusBadRequest, local: true}
}

// classifyBuildError maps request problems to 400 and RunningHub API errors
// (upload, account, queue) to their submit status so retries/refunds follow
// the usual task path.
func classifyBuildError(err error) error {
	var requestErr *mediaworkflow.RequestError
	if errors.As(err, &requestErr) {
		return localRequestError(err)
	}
	var apiErr *rh.APIError
	if errors.As(err, &apiErr) {
		return &taskBuildError{code: "runninghub_submit_failed", message: apiErr.Error(), statusCode: submitErrorStatus(apiErr.Code)}
	}
	return &taskBuildError{code: "runninghub_configuration_error", message: err.Error(), statusCode: http.StatusInternalServerError, local: true}
}
