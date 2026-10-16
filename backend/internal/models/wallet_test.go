package models

import "testing"

func TestTransferIsIncoming(t *testing.T) {
	const senderID, recipientID = 7, 42

	tests := []struct {
		name     string
		farmerID int
		want     bool
	}{
		{name: "recipient sees it as incoming", farmerID: recipientID, want: true},
		{name: "sender sees it as outgoing", farmerID: senderID, want: false},
		{name: "uninvolved user sees it as outgoing", farmerID: 999, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			transfer := Transfer{SenderID: senderID, RecipientID: recipientID}
			if got := transfer.IsIncoming(test.farmerID); got != test.want {
				t.Fatalf("IsIncoming(%d) = %v, want %v", test.farmerID, got, test.want)
			}
		})
	}
}

// TestTransferIsIncomingNeverClaimsSelfTransfer pins the odd case. A
// self-transfer is refused everywhere it could be created, but if one ever
// reached a listing it must not read as money arriving.
func TestTransferIsIncomingNeverClaimsSelfTransfer(t *testing.T) {
	transfer := Transfer{SenderID: 5, RecipientID: 5}
	if transfer.IsIncoming(5) {
		t.Fatal("a self-transfer was reported as incoming")
	}
}
