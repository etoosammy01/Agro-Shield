package models

import "testing"

func TestAdminPermissionMatrix(t *testing.T) {
	permissions := []AdminPermission{AdminEnter, AdminReadUsers, AdminManageUsers, AdminManageRoles, AdminModerateListings, AdminReadAudit, AdminReviewDisputes, AdminManageWallet}
	for _, tc := range []struct {
		name   string
		access AdminAccess
		want   []bool
	}{
		{"member", AdminAccess{}, []bool{false, false, false, false, false, false, false, false}},
		{"moderator", AdminAccess{Role: "moderator"}, []bool{true, true, false, false, true, false, false, false}},
		{"administrator", AdminAccess{Role: "administrator"}, []bool{true, true, true, true, true, true, true, false}},
		{"wallet approver", AdminAccess{Wallet: true}, []bool{true, false, false, false, false, false, false, true}},
		{"administrator with wallet", AdminAccess{Role: "administrator", Wallet: true}, []bool{true, true, true, true, true, true, true, true}},
		{"unknown role", AdminAccess{Role: "superadmin"}, []bool{false, false, false, false, false, false, false, false}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, p := range permissions {
				if tc.access.Can(p) != tc.want[i] {
					t.Errorf("permission %s = %t, want %t", p, tc.access.Can(p), tc.want[i])
				}
			}
			if tc.access.Can("unknown") {
				t.Fatal("unknown permission granted")
			}
		})
	}
}
