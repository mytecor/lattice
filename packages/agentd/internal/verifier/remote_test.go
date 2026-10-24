package verifier

import (
	"context"
	taskcontract "github.com/mytecor/lattice/task-contract"
	"os"
	"path/filepath"
	"testing"
)

func TestRemoteVerificationNeverRunsOnHost(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "must-not-exist")
	_, err := (PendingRemote{}).Verify(context.Background(), &taskcontract.TaskSpec{Checks: []string{"touch " + marker}}, "")
	if err == nil {
		t.Fatal("remote task incorrectly accepted as verified")
	}
	if _, err = os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("container check ran on host")
	}
}
