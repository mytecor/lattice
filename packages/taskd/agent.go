package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	taskcontract "github.com/mytecor/lattice/task-contract"
	"github.com/mytecor/meshbus/core"
)

// AgentCaller defines the boundary between taskd and agentd.
// taskd only knows TaskSpec -> TaskResult; it does NOT know r1s execution ID,
// ACP session, tunnel, agent iteration or lease.
type AgentCaller interface {
	RunTask(ctx context.Context, spec taskcontract.TaskSpec) (*taskcontract.TaskResult, error)
}

// MeshbusAgentCaller calls agentd over Meshbus.
type MeshbusAgentCaller struct {
	bus          Bus
	agentSubject string
	defaultTTL   time.Duration
}

// NewMeshbusAgentCaller creates a new MeshbusAgentCaller.
func NewMeshbusAgentCaller(bus Bus, agentSubject string) *MeshbusAgentCaller {
	if agentSubject == "" {
		agentSubject = taskcontract.DefaultSubjectAgentRun
	}
	return &MeshbusAgentCaller{
		bus:          bus,
		agentSubject: agentSubject,
		defaultTTL:   10 * time.Minute,
	}
}

// RunTask dispatches a TaskSpec to agentd and awaits the terminal TaskResult.
func (c *MeshbusAgentCaller) RunTask(ctx context.Context, spec taskcontract.TaskSpec) (*taskcontract.TaskResult, error) {
	reqID := randomID()
	replyTo := fmt.Sprintf("taskd.agent.reply.%s", reqID)

	respCh := make(chan *taskcontract.RunTaskResponse, 1)
	sub, err := c.bus.Subscribe(replyTo, func(_ context.Context, event core.ReceivedEvent) error {
		var resp taskcontract.RunTaskResponse
		if err := json.Unmarshal(event.Payload, &resp); err == nil && resp.ID == reqID {
			select {
			case respCh <- &resp:
			default:
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("subscribe agent reply: %w", err)
	}
	defer sub.Close()

	req := taskcontract.RunTaskRequest{
		ID:      reqID,
		Spec:    spec,
		ReplyTo: replyTo,
	}
	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal run task request: %w", err)
	}

	ttl := c.defaultTTL
	if spec.Runtime.Timeout != "" {
		if d, err := time.ParseDuration(spec.Runtime.Timeout); err == nil && d > 0 {
			ttl = d + time.Minute
		}
	}

	if _, err := c.bus.Publish(ctx, c.agentSubject, payload, core.PublishOptions{
		TTL:         ttl,
		ContentType: "application/json",
	}); err != nil {
		return nil, fmt.Errorf("publish %s: %w", c.agentSubject, err)
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-respCh:
		if resp.Error != "" {
			return nil, errors.New(resp.Error)
		}
		if resp.Result == nil {
			return nil, errors.New("received nil TaskResult from agentd")
		}
		return resp.Result, nil
	}
}
