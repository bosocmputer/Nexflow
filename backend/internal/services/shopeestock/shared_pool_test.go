package shopeestock

import (
	"errors"
	"testing"
	"time"
)

func TestRebaseSharedPoolRequestUsesFreshMappingVersions(t *testing.T) {
	oldTime := time.Date(2026, 8, 25, 1, 0, 0, 0, time.UTC)
	firstFresh := oldTime.Add(time.Minute)
	secondFresh := oldTime.Add(2 * time.Minute)
	request := SharedPoolUpdate{
		SMLItemCode:       "AH-0001",
		AutoManageMembers: true,
		Members: []SharedPoolMemberUpdate{
			{ItemID: 1, ModelID: 11, AllocationPct: 60, UpdatedAt: oldTime},
			{ItemID: 2, ModelID: 22, AllocationPct: 40, UpdatedAt: oldTime},
		},
	}
	pool := &SharedPool{SMLItemCode: "AH-0001", Members: []SharedPoolMember{
		{ItemID: 2, ModelID: 22, UpdatedAt: secondFresh},
		{ItemID: 1, ModelID: 11, UpdatedAt: firstFresh},
	}}

	got, err := rebaseSharedPoolRequest(request, pool)
	if err != nil {
		t.Fatalf("rebaseSharedPoolRequest: %v", err)
	}
	if !got.AutoManageMembers || !got.Members[0].UpdatedAt.Equal(firstFresh) || !got.Members[1].UpdatedAt.Equal(secondFresh) {
		t.Fatalf("rebased request=%+v", got)
	}
}

func TestRebaseSharedPoolRequestRejectsChangedMembership(t *testing.T) {
	request := SharedPoolUpdate{SMLItemCode: "AH-0001", Members: []SharedPoolMemberUpdate{
		{ItemID: 1, ModelID: 11, AllocationPct: 100, UpdatedAt: time.Now()},
	}}
	pool := &SharedPool{SMLItemCode: "AH-0001", Members: []SharedPoolMember{
		{ItemID: 1, ModelID: 11, UpdatedAt: time.Now()},
		{ItemID: 2, ModelID: 22, UpdatedAt: time.Now()},
	}}

	_, err := rebaseSharedPoolRequest(request, pool)
	if !errors.Is(err, ErrMappingConflict) {
		t.Fatalf("err=%v, want ErrMappingConflict", err)
	}
}
