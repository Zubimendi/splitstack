package ledger

import (
	"reflect"
	"testing"
)

func TestSplitEvenly(t *testing.T) {
	tests := []struct {
		name       string
		total      int64
		memberIDs  []string
		wantSplits []SplitInput
	}{
		{
			name:      "divides evenly",
			total:     100,
			memberIDs: []string{"A", "B"},
			wantSplits: []SplitInput{
				{UserID: "A", ShareAmountCents: 50},
				{UserID: "B", ShareAmountCents: 50},
			},
		},
		{
			name:      "handles remainder deterministically based on sorted IDs",
			total:     100,
			memberIDs: []string{"C", "A", "B"}, // 100 / 3 = 33, rem 1
			wantSplits: []SplitInput{
				{UserID: "A", ShareAmountCents: 34}, // A gets the remainder
				{UserID: "B", ShareAmountCents: 33},
				{UserID: "C", ShareAmountCents: 33},
			},
		},
		{
			name:      "handles remainder deterministically multiple cents",
			total:     101,
			memberIDs: []string{"Z", "X", "Y"}, // 101 / 3 = 33, rem 2
			wantSplits: []SplitInput{
				{UserID: "X", ShareAmountCents: 34}, // X gets remainder
				{UserID: "Y", ShareAmountCents: 34}, // Y gets remainder
				{UserID: "Z", ShareAmountCents: 33}, // Z gets base
			},
		},
		{
			name:      "single member",
			total:     150,
			memberIDs: []string{"A"},
			wantSplits: []SplitInput{
				{UserID: "A", ShareAmountCents: 150},
			},
		},
		{
			name:      "amount less than members",
			total:     2,
			memberIDs: []string{"A", "B", "C"},
			wantSplits: []SplitInput{
				{UserID: "A", ShareAmountCents: 1},
				{UserID: "B", ShareAmountCents: 1},
				{UserID: "C", ShareAmountCents: 0},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := splitEvenly(tt.total, tt.memberIDs)
			if !reflect.DeepEqual(got, tt.wantSplits) {
				t.Errorf("splitEvenly() = %v, want %v", got, tt.wantSplits)
			}
		})
	}
}

func TestValidateSplits(t *testing.T) {
	members := map[string]bool{"A": true, "B": true}

	t.Run("valid splits", func(t *testing.T) {
		splits := []SplitInput{
			{UserID: "A", ShareAmountCents: 50},
			{UserID: "B", ShareAmountCents: 50},
		}
		err := validateSplits(splits, 100, members)
		if err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("splits don't sum to total", func(t *testing.T) {
		splits := []SplitInput{
			{UserID: "A", ShareAmountCents: 50},
			{UserID: "B", ShareAmountCents: 40},
		}
		err := validateSplits(splits, 100, members)
		if err != ErrSplitsDontSum {
			t.Errorf("expected ErrSplitsDontSum, got %v", err)
		}
	})

	t.Run("not a group member", func(t *testing.T) {
		splits := []SplitInput{
			{UserID: "A", ShareAmountCents: 50},
			{UserID: "C", ShareAmountCents: 50},
		}
		err := validateSplits(splits, 100, members)
		if err != ErrNotGroupMember {
			t.Errorf("expected ErrNotGroupMember, got %v", err)
		}
	})

	t.Run("zero share amount", func(t *testing.T) {
		splits := []SplitInput{
			{UserID: "A", ShareAmountCents: 100},
			{UserID: "B", ShareAmountCents: 0},
		}
		err := validateSplits(splits, 100, members)
		if err != ErrSplitsDontSum {
			t.Errorf("expected ErrSplitsDontSum, got %v", err)
		}
	})

	t.Run("empty splits", func(t *testing.T) {
		err := validateSplits([]SplitInput{}, 100, members)
		if err != ErrSplitsDontSum {
			t.Errorf("expected ErrSplitsDontSum, got %v", err)
		}
	})
}
