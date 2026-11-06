package cli

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/mexcdevelop/mexc-cli/pkg/confirmaudit"
	"github.com/mexcdevelop/mexc-cli/pkg/credential"
)

// slowElicitor models the human taking time over the dialog, which is what lets a
// judgement's two records be raised far apart.
type slowElicitor struct{}

func (slowElicitor) Elicit(ctx context.Context, p *mcp.ElicitParams) (*mcp.ElicitResult, error) {
	time.Sleep(700 * time.Millisecond)
	return &mcp.ElicitResult{Action: "accept"}, nil
}

// slowRep models a real endpoint round trip.
type slowRep struct {
	mu   sync.Mutex
	got  []confirmaudit.Event
	wait time.Duration
}

func (s *slowRep) Report(ctx context.Context, ev confirmaudit.Event) error {
	select {
	case <-time.After(s.wait):
	case <-ctx.Done():
		return ctx.Err()
	}
	s.mu.Lock()
	s.got = append(s.got, ev)
	s.mu.Unlock()
	return nil
}

// Concurrent judgements — the MCP face serves tool calls in parallel — must lose
// nothing. A dropped reminder whose confirmation survived reads as a self-approving
// caller: the finding this trail exists to produce, manufactured against someone
// who did nothing wrong. Sends must therefore not be routed through anything with a
// shared capacity.
//
// Order ACROSS judgements is not asserted, only within one (see stages): concurrent
// callers interleave, and no audit conclusion depends on which of them lands first.
func TestConfirmAudit_ConcurrentJudgementsLoseNothing(t *testing.T) {
	for round := 0; round < 3; round++ {
		n := 24
		rep := &slowRep{wait: 150 * time.Millisecond}
		f := auditFactory(rep)
		op := writeOp()
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				base := auditEvent(f, op, map[string]string{"symbol": fmt.Sprintf("S%d", i)}, "", "", faceMCP, credential.ConfirmGrade{})
				ch, err := mcpConfirm(context.Background(), f, base, slowElicitor{}, op, "m", false)
				if err == nil {
					// What toolHandler defers.
					reportConfirmed(f, base, ch)
				}
			}(i)
		}
		wg.Wait()

		order := map[string][]confirmaudit.Stage{}
		rep.mu.Lock()
		for _, e := range rep.got {
			order[e.InvocationID] = append(order[e.InvocationID], e.Stage)
		}
		delivered := len(rep.got)
		rep.mu.Unlock()

		// A judgement missing its reminder is what an audit reads as self-approval.
		falseSelf := 0
		for _, seq := range order {
			if len(seq) == 1 && seq[0] == confirmaudit.StageConfirmed {
				falseSelf++
			}
		}
		t.Logf("round %d: delivered %d/%d judgements=%d WOULD-READ-AS-SELF-APPROVAL=%d",
			round, delivered, n*2, len(order), falseSelf)
		if delivered != n*2 {
			t.Errorf("round %d: dropped %d records", round, n*2-delivered)
		}
		if falseSelf > 0 {
			t.Errorf("round %d: %d judgements lost their reminder", round, falseSelf)
		}
	}
}
