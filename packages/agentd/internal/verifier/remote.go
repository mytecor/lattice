package verifier

import (
	"context"
	"errors"
	taskcontract "github.com/mytecor/lattice/task-contract"
)

// PendingRemote prevents host-side checks and false task completion until the
// container verifier/result publication contract is implemented in f19-03.
type PendingRemote struct{}

func (PendingRemote) Verify(context.Context, *taskcontract.TaskSpec, string) (*Result, error) {
	return nil, errors.New("remote verification and source result collection require f19-03")
}
