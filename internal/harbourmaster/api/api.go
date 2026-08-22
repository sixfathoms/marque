// Package api serves the Harbourmaster's Connect surface over the store.
//
// It decides nothing. M1's Harbourmaster records what an operator asked for,
// records that someone said they approved it, and records what the Pilot
// reported — no parsing, no scoping, no rehearsal, and no signature. What makes
// that safe to have written is that it is gated behind an acknowledgement and
// that the record says so; what makes it worth having written is that it proves
// the six steps join up.
//
// EDR-0005's boundary shows in what is absent: no target driver, no target
// credential, no connection to anything but the control plane's own database.
package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"connectrpc.com/connect"

	v1 "github.com/sixfathoms/marque/gen/marque/v1"
	"github.com/sixfathoms/marque/gen/marque/v1/marquev1connect"
	"github.com/sixfathoms/marque/internal/harbourmaster/store"
	"github.com/sixfathoms/marque/internal/version"
)

// Requests is the part of the store this service uses.
//
// An interface so the validation and the error mapping can be tested without a
// database. Not a general abstraction over storage — there is one
// implementation and EDR-0013 fixed the engine — so it names exactly the four
// calls and nothing more.
type Requests interface {
	Submit(ctx context.Context, tenant string, r store.Request) (string, error)
	Request(ctx context.Context, tenant, reference string) (store.Request, error)
	Approve(ctx context.Context, tenant, reference, approver string, stage uint32) error
	RecordExecution(ctx context.Context, tenant, reference string, e store.Execution) (store.Execution, error)
}

// Service implements marquev1connect.HarbourmasterServiceHandler.
type Service struct {
	store Requests

	// tenant is configuration, not a request field.
	//
	// EDR-0025 puts the tenant on the authenticated principal, and M1 has no
	// identity at all, so it comes from configuration here. That is a change of
	// SOURCE at M4 and not a change of schema, which is why tenant_id is in the
	// first migration. It is deliberately not a field on any request message: a
	// tenant a caller can choose is not a tenant.
	tenant string

	// submitter is likewise not a field. M1 has nobody to authenticate, so
	// every request records the same string, and it says what it is.
	submitter string
}

var _ marquev1connect.HarbourmasterServiceHandler = (*Service)(nil)

// New builds the service over a store.
func New(s Requests, tenant string) *Service {
	return &Service{store: s, tenant: tenant, submitter: "unauthenticated"}
}

// GetVersion reports the software, and is the one method that touches nothing.
func (s *Service) GetVersion(
	_ context.Context, _ *connect.Request[v1.GetVersionRequest],
) (*connect.Response[v1.GetVersionResponse], error) {
	i := version.Get()
	return connect.NewResponse(&v1.GetVersionResponse{
		Version:    i.Version,
		Commit:     i.Commit,
		SourceDate: i.SourceDate,
		GoVersion:  i.Go,
		Platform:   i.Platform,
	}), nil
}

// Submit records a statement an operator wants to run, and decides nothing
// about it. Keyed on the caller's idempotency key, so a retried submission is
// one request.
func (s *Service) Submit(
	ctx context.Context, req *connect.Request[v1.SubmitRequest],
) (*connect.Response[v1.SubmitResponse], error) {
	m := req.Msg
	// Validated here rather than left to the schema's CHECK constraints: a
	// constraint violation arrives as an opaque database error, which a client
	// cannot act on and which this would have to report as Internal. These are
	// the caller's mistakes and they get InvalidArgument.
	for _, f := range []struct{ name, value string }{
		{"statement", m.GetStatement()},
		{"target", m.GetTarget()},
		{"role", m.GetRole()},
		{"reason", m.GetReason()},
		{"idempotency_key", m.GetIdempotencyKey()},
	} {
		if strings.TrimSpace(f.value) == "" {
			return nil, connect.NewError(connect.CodeInvalidArgument,
				fmt.Errorf("%s is required", f.name))
		}
	}

	reference, err := s.store.Submit(ctx, s.tenant, store.Request{
		Statement:      m.GetStatement(),
		Target:         m.GetTarget(),
		Role:           m.GetRole(),
		Reason:         m.GetReason(),
		Submitter:      s.submitter,
		IdempotencyKey: m.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, asConnectError(err)
	}
	return connect.NewResponse(&v1.SubmitResponse{Reference: reference}), nil
}

// GetRequest returns one request. An unknown reference and another tenant's
// reference are the same answer, because a reference must not confirm its own
// existence (EDR-0038).
func (s *Service) GetRequest(
	ctx context.Context, req *connect.Request[v1.GetRequestRequest],
) (*connect.Response[v1.GetRequestResponse], error) {
	r, err := s.store.Request(ctx, s.tenant, req.Msg.GetReference())
	if err != nil {
		return nil, asConnectError(err)
	}
	wire, err := asProto(r)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.GetRequestResponse{Request: wire}), nil
}

// asProto turns a stored request into its wire form.
//
// A state the database holds and this build does not know is Internal, not a
// client error: the vocabulary is closed and both ends are supposed to have the
// same one, so the honest answer is that this build is wrong.
func asProto(r store.Request) (*v1.Request, error) {
	state, ok := stateToProto[r.State]
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("stored state %q is not one this build knows", r.State))
	}
	return &v1.Request{
		Reference: r.Reference,
		Statement: r.Statement,
		Target:    r.Target,
		Role:      r.Role,
		Submitter: r.Submitter,
		Reason:    r.Reason,
		State:     state,
	}, nil
}

// Approve records that someone said they approved a request. At M1 that is an
// assertion by the caller and nothing more: there is no signature and no
// identity behind it, which is what the startup banner is for.
func (s *Service) Approve(
	ctx context.Context, req *connect.Request[v1.ApproveRequest],
) (*connect.Response[v1.ApproveResponse], error) {
	m := req.Msg
	if strings.TrimSpace(m.GetApprover()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("approver is required"))
	}
	// M1 has no escalation chain, so stage 1 is the only stage there is. A
	// caller sending 2 is asserting something this cannot honour, and silently
	// accepting it would record an approval at a stage no policy defines.
	if m.GetStage() != 1 {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("stage must be 1: M1 has no escalation chain, and EDR-0030's per-stage thresholds arrive with signing at M3 (got %d)", m.GetStage()))
	}
	if err := s.store.Approve(ctx, s.tenant, m.GetReference(), m.GetApprover(), m.GetStage()); err != nil {
		return nil, asConnectError(err)
	}
	// The response carries the request, because the proto says it does. A
	// caller that has to issue a second GetRequest to learn the new state is a
	// caller racing anyone else who can change it.
	r, err := s.store.Request(ctx, s.tenant, m.GetReference())
	if err != nil {
		return nil, asConnectError(err)
	}
	wire, err := asProto(r)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.ApproveResponse{Request: wire}), nil
}

// RecordExecution stores one attempt's report and returns what is STORED,
// which is not always what was sent: a repeated nonce returns the first
// outcome, so a Pilot retrying after a timeout learns what was recorded rather
// than believing its own second answer.
func (s *Service) RecordExecution(
	ctx context.Context, req *connect.Request[v1.RecordExecutionRequest],
) (*connect.Response[v1.RecordExecutionResponse], error) {
	m := req.Msg
	if strings.TrimSpace(m.GetNonce()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("nonce is required"))
	}
	outcome, ok := outcomeFromProto[m.GetOutcome()]
	if !ok {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			fmt.Errorf("outcome %s is not one of the four EDR-0042 decides", m.GetOutcome()))
	}
	// The biconditional the schema also enforces: rows_affected is absent
	// exactly when the outcome is indeterminate. Checked here so a caller gets
	// InvalidArgument rather than a CHECK violation reported as Internal — and
	// checked there too, because this is not the only writer forever.
	indeterminate := outcome == store.OutcomeIndeterminate
	if indeterminate != (m.RowsAffected == nil) {
		return nil, connect.NewError(connect.CodeInvalidArgument,
			errors.New("rows_affected must be absent exactly when the outcome is indeterminate: an outcome meaning nobody knows must not carry a count"))
	}

	stored, err := s.store.RecordExecution(ctx, s.tenant, m.GetReference(), store.Execution{
		Nonce:        m.GetNonce(),
		Outcome:      outcome,
		RowsAffected: m.RowsAffected,
	})
	if err != nil {
		return nil, asConnectError(err)
	}
	proto, ok := outcomeToProto[stored.Outcome]
	if !ok {
		return nil, connect.NewError(connect.CodeInternal,
			fmt.Errorf("stored outcome %q is not one this build knows", stored.Outcome))
	}
	r, err := s.store.Request(ctx, s.tenant, m.GetReference())
	if err != nil {
		return nil, asConnectError(err)
	}
	wire, err := asProto(r)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&v1.RecordExecutionResponse{
		Request: wire,
		Execution: &v1.Execution{
			Nonce:        stored.Nonce,
			Outcome:      proto,
			RowsAffected: stored.RowsAffected,
		},
	}), nil
}

// asConnectError maps the store's named errors onto codes a client can act on.
//
// NotFound for an unknown reference, never PermissionDenied: EDR-0038 says a
// reference must not confirm its own existence, so "you may not see this" and
// "this is not here" have to be the same answer.
func asConnectError(err error) error {
	switch {
	case errors.Is(err, store.ErrNoSuchRequest):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, store.ErrWrongState):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	default:
		return connect.NewError(connect.CodeInternal, err)
	}
}
