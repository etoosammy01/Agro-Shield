package onboarding

import "sync/atomic"

type policy struct {
	skip     bool
	allUsers bool
	userIDs  map[int]struct{}
}

var configured atomic.Pointer[policy]

// Configure installs the development-only policy validated at startup. An empty
// ID list applies to all development accounts; otherwise only listed users skip.
func Configure(skip bool, userIDs []int) {
	p := &policy{skip: skip, allUsers: len(userIDs) == 0, userIDs: make(map[int]struct{}, len(userIDs))}
	for _, id := range userIDs {
		if id > 0 {
			p.userIDs[id] = struct{}{}
		}
	}
	configured.Store(p)
}

// CanSkip controls navigation and access without creating verification evidence.
// Authentication, suspension checks and administrative permissions still apply.
func CanSkip(userID int) bool {
	p := configured.Load()
	if p == nil || !p.skip || userID <= 0 {
		return false
	}
	if p.allUsers {
		return true
	}
	_, allowed := p.userIDs[userID]
	return allowed
}
