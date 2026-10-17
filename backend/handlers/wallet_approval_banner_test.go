package handlers

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"backend/internal/services"
)

// Each refusal an admin can act on needs its own instruction, because the
// generic banner tells them to check the status and retry - which is right for a
// status race and wrong for all of these. Two of them matter more than wording:
// a refused payout needs Reject, and a payout already at the provider must be
// steered away from Reject and from the way back to the queue.
func TestApprovalFailureForDirectsEachRefusal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			// The service's own suffix is what points at Reject, so the test
			// uses the error as the approval path really builds it.
			name: "details that do not match",
			err: fmt.Errorf("%w: the bank has %q registered on this account. Reject this request to release the reserved funds",
				services.ErrPayoutDetailsMismatch, "ADAEZE OKONKWO"),
			want: "Reject",
		},
		{
			name: "approving your own request",
			err:  services.ErrSelfApproval,
			want: "another admin",
		},
		{
			name: "a payout Flutterwave refused",
			err:  fmt.Errorf("%w: the request is back in the queue to reject", services.ErrTransferNotCreated),
			want: "Reject it",
		},
		{
			// The one that must never be answered with "reject": the money is
			// already on its way.
			name: "a payout submitted but not recorded",
			err:  fmt.Errorf("%w: transfer 999 (withdrawal WITHDRAW-7-1)", services.ErrPayoutSubmitted),
			want: "Do not reject",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			banner := approvalFailureFor(test.err)
			if banner == "" {
				t.Fatalf("approvalFailureFor(%v) = \"\", want an instruction", test.err)
			}
			if !strings.Contains(banner, test.want) {
				t.Errorf("approvalFailureFor(%v) = %q, want it to contain %q", test.err, banner, test.want)
			}
		})
	}
}

// Anything unrecognised has to fall back to the generic text rather than
// guessing, since a wrong instruction here moves money the wrong way.
func TestApprovalFailureForLeavesUnknownFailuresAlone(t *testing.T) {
	for _, err := range []error{
		errors.New("connection reset by peer"),
		services.ErrWalletUnavailable,
		nil,
	} {
		if banner := approvalFailureFor(err); banner != "" {
			t.Errorf("approvalFailureFor(%v) = %q, want the generic banner", err, banner)
		}
	}
}
